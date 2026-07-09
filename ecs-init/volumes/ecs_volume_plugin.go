// Copyright 2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package volumes

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/aws/amazon-ecs-agent/ecs-init/volumes/driver"
	"github.com/aws/amazon-ecs-agent/ecs-init/volumes/types"
	"github.com/cihub/seelog"
	"github.com/docker/go-plugins-helpers/volume"
)

const (
	// VolumeMountPathPrefix is the host path where amazon ECS plugin's volumes are mounted
	VolumeMountPathPrefix = "/var/lib/ecs/volumes/"
	// FilePerm is the file permissions for the host volume mount directory
	FilePerm          = 0700
	defaultDriverType = "efs"
)

// AmazonECSVolumePlugin holds the volume drivers and the set of managed volumes.
//
// Each volume is owned by a volumeWorker (see ecs_volume_plugin_worker.go) that
// serializes mount, unmount and remove operations for that volume on its own
// goroutine. Operations on different volumes therefore proceed concurrently and
// are never serialized behind a single lock while a mount/unmount syscall is in
// flight.
type AmazonECSVolumePlugin struct {
	volumeDrivers map[string]driver.VolumeDriver
	volumes       map[string]*types.Volume
	state         *StateManager
	workers       map[string]*volumeWorker
	// volumesLock guards the volumes and workers maps. It is held only for fast
	// map lookups and mutations, never while a mount/unmount syscall is in flight,
	// so that per-volume I/O on the worker goroutines is never serialized behind it.
	volumesLock sync.RWMutex
}

// NewAmazonECSVolumePlugin initiates the volume drivers
func NewAmazonECSVolumePlugin() *AmazonECSVolumePlugin {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs":     NewECSVolumeDriver(),
			"s3files": NewECSVolumeDriver(),
		},
		volumes: make(map[string]*types.Volume),
		state:   NewStateManager(),
		workers: make(map[string]*volumeWorker),
	}
	return plugin
}

// getOrCreateWorker returns the worker that owns the named volume, creating and
// starting one on demand. It returns nil if the volume does not exist. It uses
// double-checked locking so the common (already-exists) path takes only a read
// lock, and never holds volumesLock while dispatching to or waiting on a worker.
func (a *AmazonECSVolumePlugin) getOrCreateWorker(name string) *volumeWorker {
	a.volumesLock.RLock()
	vw := a.workers[name]
	a.volumesLock.RUnlock()
	if vw != nil {
		return vw
	}

	a.volumesLock.Lock()
	defer a.volumesLock.Unlock()
	if vw = a.workers[name]; vw != nil {
		return vw
	}
	vol, ok := a.volumes[name]
	if !ok {
		return nil
	}
	// getVolumeDriver only fails for an unsupported type, which Create rejects
	// before registering a volume; a nil driver is handled by the worker.
	volDriver, _ := a.getVolumeDriver(vol.Type)
	vw = &volumeWorker{
		name:    name,
		vol:     vol,
		driver:  volDriver,
		state:   a.state,
		cleanup: a.CleanupMountPath,
		mailbox: make(chan func(), volumeWorkerMailboxSize),
		stopped: make(chan struct{}),
	}
	a.workers[name] = vw
	go vw.run()
	return vw
}

// LoadState loads past state information of the plugin
func (a *AmazonECSVolumePlugin) LoadState() error {
	a.volumesLock.Lock()
	defer a.volumesLock.Unlock()
	seelog.Info("Loading plugin state information")
	oldState := &VolumeState{}
	if !fileExists(PluginStateFileAbsPath) {
		return nil
	}
	if err := a.state.load(oldState); err != nil {
		seelog.Errorf("Could not load state: %v", err)
		return fmt.Errorf("could not load plugin state: %v", err)
	}
	// empty state file
	if oldState.Volumes == nil {
		return nil
	}

	// Reset volume mount reference count. This is for backwards-compatibility with old
	// state file format which did not have reference counting of volume mounts.
	for _, vol := range oldState.Volumes {
		for mountId, count := range vol.Mounts {
			if count == 0 {
				vol.Mounts[mountId] = 1
			}
		}
	}

	for volName, vol := range oldState.Volumes {
		voldriver, err := a.getVolumeDriver(vol.Type)
		if err != nil {
			seelog.Errorf("Could not load state: %v", err)
			return fmt.Errorf("could not load plugin state: %v", err)
		}
		volume := &types.Volume{
			Type:      vol.Type,
			Path:      vol.Path,
			Options:   vol.Options,
			CreatedAt: vol.CreatedAt,
			Mounts:    vol.Mounts,
		}
		a.volumes[volName] = volume
		voldriver.Setup(volName, volume)
	}
	a.state.VolState = oldState
	return nil
}

func (a *AmazonECSVolumePlugin) getVolumeDriver(driverType string) (driver.VolumeDriver, error) {
	if driverType == "" {
		return a.volumeDrivers[defaultDriverType], nil
	}
	if _, ok := a.volumeDrivers[driverType]; !ok {
		return nil, fmt.Errorf("volume %s type not supported", driverType)
	}
	return a.volumeDrivers[driverType], nil
}

// Create implements Docker volume plugin's Create Method.
// Create is metadata-only (no mount I/O), so it runs under the registry lock.
// The per-volume worker is created lazily on the first Mount/Unmount/Remove.
func (a *AmazonECSVolumePlugin) Create(r *volume.CreateRequest) error {
	a.volumesLock.Lock()
	defer a.volumesLock.Unlock()

	seelog.Infof("Creating new volume %s", r.Name)
	_, ok := a.volumes[r.Name]
	if ok {
		return fmt.Errorf("volume %s already exists", r.Name)
	}

	// get driver type from options to get the corresponding volume driver
	var driverType, target string
	for k, v := range r.Options {
		switch k {
		case "type":
			driverType = v
		case "target":
			target = v
		}
	}
	volDriver, err := a.getVolumeDriver(driverType)
	if err != nil {
		seelog.Errorf("Volume %s's driver type %s not supported", r.Name, driverType)
		return err
	}
	if volDriver == nil {
		// this case should not happen normally
		return fmt.Errorf("no volume driver found for type %s", driverType)
	}

	if target == "" {
		seelog.Infof("Creating mount target for new volume %s", r.Name)
		// create the mount path on the host for the volume to be created
		target, err = a.GetMountPath(r.Name)
		if err != nil {
			seelog.Errorf("Volume %s creation failure: %v", r.Name, err)
			return err
		}
	}

	vol := &types.Volume{
		Type:      driverType,
		Path:      target,
		Options:   r.Options,
		CreatedAt: time.Now().Format(time.RFC3339Nano),
		Mounts:    map[string]int{},
	}
	// record the volume information
	a.volumes[r.Name] = vol
	seelog.Infof("Saving state of new volume %s", r.Name)
	// save the state of new volume
	err = a.state.recordVolume(r.Name, vol)
	if err != nil {
		seelog.Errorf("Error saving state of new volume %s: %v", r.Name, err)
	}
	return nil
}

// GetMountPath returns the host path where volume will be mounted
func (a *AmazonECSVolumePlugin) GetMountPath(name string) (string, error) {
	path := VolumeMountPathPrefix + name
	err := createMountPath(path)
	if err != nil {
		return "", fmt.Errorf("cannot create mount point: %v", err)
	}
	return path, nil
}

var createMountPath = createMountDir

func createMountDir(path string) error {
	return os.MkdirAll(path, FilePerm)
}

// CleanupMountPath cleans up the volume's host path
func (a *AmazonECSVolumePlugin) CleanupMountPath(name string) error {
	return removeMountPath(name)
}

var removeMountPath = deleteMountPath

func deleteMountPath(path string) error {
	return os.Remove(path)
}

// Mount implements Docker volume plugin's Mount Method.
// The mount I/O is executed on the volume's worker goroutine, so mounts for
// different volumes proceed concurrently while mounts for the same volume are
// serialized.
func (a *AmazonECSVolumePlugin) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	seelog.Infof("Received mount request %+v", r)

	// Validate the request
	if len(r.Name) == 0 {
		return nil, fmt.Errorf("no volume in the request")
	}
	if len(r.ID) == 0 {
		return nil, fmt.Errorf("no mount ID in the request")
	}

	vw := a.getOrCreateWorker(r.Name)
	if vw == nil {
		seelog.Errorf("Volume %s to mount is not found", r.Name)
		return nil, fmt.Errorf("volume %s not found", r.Name)
	}

	var resp *volume.MountResponse
	var mErr error
	if !vw.do(func() { resp, mErr = vw.mount(r.ID) }) {
		return nil, fmt.Errorf("volume %s was removed", r.Name)
	}
	return resp, mErr
}

// Unmount implements Docker volume plugin's Unmount Method.
// The unmount I/O is executed on the volume's worker goroutine.
func (a *AmazonECSVolumePlugin) Unmount(r *volume.UnmountRequest) error {
	seelog.Infof("Received unmount request %+v", r)

	// Validate the request
	if len(r.Name) == 0 {
		return fmt.Errorf("no volume in the request")
	}
	if len(r.ID) == 0 {
		return fmt.Errorf("no mount ID in the request")
	}

	vw := a.getOrCreateWorker(r.Name)
	if vw == nil {
		seelog.Errorf("Volume %s to unmount is not found", r.Name)
		return fmt.Errorf("volume %s not found", r.Name)
	}

	var uErr error
	if !vw.do(func() { uErr = vw.unmount(r.ID) }) {
		return fmt.Errorf("volume %s was removed", r.Name)
	}
	return uErr
}

// Remove implements Docker volume plugin's Remove Method.
// The removal is dispatched to the volume's worker so it serializes behind any
// in-flight Mount/Unmount for the same volume. On success the worker is stopped
// and the volume is dropped from the registry.
func (a *AmazonECSVolumePlugin) Remove(r *volume.RemoveRequest) error {
	seelog.Infof("Received Remove request %+v", r)

	vw := a.getOrCreateWorker(r.Name)
	if vw == nil {
		seelog.Errorf("Volume %s to remove is not found", r.Name)
		return fmt.Errorf("volume %s not found", r.Name)
	}

	var rmErr error
	if !vw.do(func() {
		rmErr = vw.remove()
		if rmErr == nil {
			// Ask the worker loop to exit after this message.
			vw.stop = true
		}
	}) {
		// Worker already stopped: the volume was concurrently removed.
		return nil
	}
	if rmErr != nil {
		// Keep the volume and its worker intact on failure.
		return rmErr
	}

	a.volumesLock.Lock()
	delete(a.workers, r.Name)
	delete(a.volumes, r.Name)
	a.volumesLock.Unlock()
	return nil
}

// List implements Docker volume plugin's List Method
func (a *AmazonECSVolumePlugin) List() (*volume.ListResponse, error) {
	a.volumesLock.RLock()
	defer a.volumesLock.RUnlock()
	vols := make([]*volume.Volume, len(a.volumes))
	i := 0
	for volName := range a.volumes {
		vols[i] = &volume.Volume{
			Name: volName,
		}
		i++
	}
	res := &volume.ListResponse{
		Volumes: vols,
	}
	return res, nil
}

// Get implements Docker volume plugin's Get Method
func (a *AmazonECSVolumePlugin) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	a.volumesLock.RLock()
	defer a.volumesLock.RUnlock()
	vol, ok := a.volumes[r.Name]
	if !ok {
		return nil, fmt.Errorf("volume %s not found", r.Name)
	}
	resp := &volume.Volume{
		Name:       r.Name,
		Mountpoint: vol.Path,
		CreatedAt:  vol.CreatedAt,
	}
	seelog.Infof("Returning volume information for %s", resp.Name)
	return &volume.GetResponse{Volume: resp}, nil
}

// Path implements Docker volume plugin's Path Method
func (a *AmazonECSVolumePlugin) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	a.volumesLock.RLock()
	defer a.volumesLock.RUnlock()
	vol, ok := a.volumes[r.Name]
	if !ok {
		seelog.Errorf("Could not find mount path for volume %s", r.Name)
		return nil, fmt.Errorf("volume %s not found", r.Name)
	}

	return &volume.PathResponse{Mountpoint: vol.Path}, nil
}

// Capabilities implements Docker volume plugin's Capabilities Method
func (a *AmazonECSVolumePlugin) Capabilities() *volume.CapabilitiesResponse {
	// Note: This is currently not supported
	return nil
}
