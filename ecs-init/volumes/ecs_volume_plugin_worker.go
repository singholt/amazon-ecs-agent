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

	"github.com/aws/amazon-ecs-agent/ecs-init/volumes/driver"
	"github.com/aws/amazon-ecs-agent/ecs-init/volumes/types"
	"github.com/cihub/seelog"
	"github.com/docker/go-plugins-helpers/volume"
)

// volumeWorkerMailboxSize is the buffer size of a per-volume worker's mailbox.
const volumeWorkerMailboxSize = 8

// volumeWorker owns a single volume and serializes all mount, unmount and remove
// operations on it. Requests are submitted as closures to its mailbox and run
// one at a time on a single goroutine, so the worker has exclusive access to the
// volume's mutable state without additional locking. Operations on different
// volumes run on separate workers and therefore proceed concurrently.
type volumeWorker struct {
	name string
	vol  *types.Volume
	// driver is the volume driver, resolved once from the volume type when the
	// worker is created. The volume type does not change during the volume's
	// lifetime.
	driver driver.VolumeDriver
	// state persists volume state. It is shared across workers and is safe for
	// concurrent use.
	state *StateManager
	// cleanup removes the volume's host mount path.
	cleanup func(path string) error
	// mailbox carries operations to be executed on the worker goroutine.
	mailbox chan func()
	// stopped is closed when the worker goroutine has exited.
	stopped chan struct{}
	// stop is accessed only on the worker goroutine. When set true after
	// processing a message, the worker loop exits.
	stop bool
}

// run is the worker's event loop. It executes queued closures serially until it
// is asked to stop (via a message that sets stop) or its mailbox is closed.
func (vw *volumeWorker) run() {
	defer close(vw.stopped)
	for f := range vw.mailbox {
		f()
		if vw.stop {
			return
		}
	}
}

// do submits fn to the worker and blocks until fn has executed. It returns false
// if the worker has already stopped, in which case fn is not guaranteed to have
// run. Blocking the caller is expected: the Docker VolumeDriver API is synchronous.
func (vw *volumeWorker) do(fn func()) bool {
	ack := make(chan struct{})
	msg := func() {
		defer close(ack)
		fn()
	}
	select {
	case vw.mailbox <- msg:
	case <-vw.stopped:
		return false
	}
	select {
	case <-ack:
		return true
	case <-vw.stopped:
		return false
	}
}

// mount performs the mount I/O and state update for a new mount reference. It
// runs on the worker goroutine and has exclusive access to the volume.
func (vw *volumeWorker) mount(id string) (*volume.MountResponse, error) {
	volDriver := vw.driver
	if volDriver == nil {
		return nil, fmt.Errorf("no volume driver found for type %s", vw.vol.Type)
	}

	// Mount the volume on the host if there are no active mounts for the volume.
	if len(vw.vol.Mounts) == 0 {
		seelog.Infof("Mounting volume %s as there are no existing mounts for it", vw.name)
		createReq := &driver.CreateRequest{Name: vw.name, Path: vw.vol.Path, Options: vw.vol.Options}
		if err := volDriver.Create(createReq); err != nil {
			seelog.Errorf("Volume %s creation failure: %v", vw.name, err)
			return nil, fmt.Errorf("failed to mount volume %s: %w", vw.name, err)
		}
		seelog.Infof("Volume %s mounted successfully", vw.name)
	}

	// Update state
	seelog.Infof("Adding mount %s to volume %s", id, vw.name)
	vw.vol.AddMount(id)
	if err := vw.state.recordVolume(vw.name, vw.vol); err != nil {
		// State update failed, so roll back the changes made so far to make state consistent
		seelog.Errorf("Failed to save volume %s, rolling back changes: %v", vw.name, err)
		vw.vol.RemoveMount(id)
		if len(vw.vol.Mounts) == 0 {
			seelog.Warnf("Rolling back mounting of volume %s", vw.name)
			if err := volDriver.Remove(&driver.RemoveRequest{Name: vw.name}); err != nil {
				seelog.Errorf("Volume %s removal failure: %v", vw.name, err)
			}
		}
		vw.state.recordVolume(vw.name, vw.vol)
		return nil, fmt.Errorf("mount failed due to an error while saving state: %w", err)
	}

	// All good
	return &volume.MountResponse{Mountpoint: vw.vol.Path}, nil
}

// unmount removes a mount reference, unmounting the volume from the host once no
// references remain. It runs on the worker goroutine with exclusive access to
// the volume.
func (vw *volumeWorker) unmount(id string) error {
	volDriver := vw.driver
	if volDriver == nil {
		return fmt.Errorf("no corresponding volume driver found for type %s", vw.vol.Type)
	}

	// Remove the mount from the volume
	seelog.Infof("Removing mount %s from volume %s", id, vw.name)
	if exists := vw.vol.RemoveMount(id); !exists {
		seelog.Warnf("Mount %s was not found on volume %s, this is a no-op", id, vw.name)
		return nil
	}

	// If there are no more mounts left on the volume then unmount the volume from the host
	if len(vw.vol.Mounts) == 0 {
		seelog.Infof("No active mounts left on volume %s, unmounting it", vw.name)
		if err := volDriver.Remove(&driver.RemoveRequest{Name: vw.name}); err != nil {
			seelog.Errorf("Failed to unmount volume %v: %v", vw.name, err)
			return fmt.Errorf("failed to unmount volume %v: %w", vw.name, err)
		}
	}

	// Save state
	if err := vw.state.recordVolume(vw.name, vw.vol); err != nil {
		// State save failed, so roll back the changes made so far to make state consistent
		seelog.Errorf("Error saving state of volume %s: %v", vw.name, err)
	}

	// All good
	return nil
}

// remove unmounts the volume if it is still mounted, cleans up its host mount
// path and removes its persisted state. It runs on the worker goroutine with
// exclusive access to the volume.
func (vw *volumeWorker) remove() error {
	seelog.Infof("Removing volume %s", vw.name)

	volDriver := vw.driver
	if volDriver == nil {
		return fmt.Errorf("no corresponding volume driver found for type %s", vw.vol.Type)
	}

	// Although unmounts are handled by Unmount method, unmount the volume if it's still
	// mounted. This is mainly to unmount volumes created by an older version of the
	// plugin in which unmounts were not handled by Unmount method.
	if volDriver.IsMounted(vw.name) {
		seelog.Infof("Volume %s is currently mounted, unmounting it", vw.name)
		if err := volDriver.Remove(&driver.RemoveRequest{Name: vw.name}); err != nil {
			seelog.Errorf("Volume %s removal failure: %v", vw.name, err)
			return err
		}
	}

	// cleanup the volume's host mount path
	if err := vw.cleanup(vw.vol.Path); err != nil {
		seelog.Errorf("Cleaning mount path failed for volume %s: %v", vw.name, err)
	}
	seelog.Infof("Saving state after removing volume %s", vw.name)
	// remove the state of deleted volume
	if err := vw.state.removeVolume(vw.name); err != nil {
		seelog.Errorf("Error saving state after removing volume %s: %v", vw.name, err)
	}
	return nil
}
