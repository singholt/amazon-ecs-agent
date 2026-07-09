// Copyright 2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package volumes

import (
	"errors"
	"testing"

	"github.com/aws/amazon-ecs-agent/ecs-init/volumes/driver"
	"github.com/aws/amazon-ecs-agent/ecs-init/volumes/types"
	"github.com/docker/go-plugins-helpers/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVolumeDriver implements VolumeDriver interface for testing
type TestVolumeDriver struct{}

func NewTestVolumeDriver() *TestVolumeDriver {
	return &TestVolumeDriver{}
}

func (t *TestVolumeDriver) Create(r *driver.CreateRequest) error {
	return nil
}

func (t *TestVolumeDriver) Remove(r *driver.RemoveRequest) error {
	return nil
}

func (t *TestVolumeDriver) Setup(n string, v *types.Volume) {
	return
}

func (t *TestVolumeDriver) IsMounted(volumeName string) bool {
	return false
}

// TestVolumeDriverError implements VolumeDriver interface for testing
// Returns error for all methods
type TestVolumeDriverError struct{}

func NewTestVolumeDriverError() *TestVolumeDriverError {
	return &TestVolumeDriverError{}
}

func (t *TestVolumeDriverError) Create(r *driver.CreateRequest) error {
	return errors.New("create error")
}

func (t *TestVolumeDriverError) Remove(r *driver.RemoveRequest) error {
	return errors.New("remove error")
}

func (t *TestVolumeDriverError) Setup(n string, v *types.Volume) {
	return
}

func (t *TestVolumeDriverError) IsMounted(volumeName string) bool {
	return false
}

func TestVolumeCreateHappyPath(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": NewTestVolumeDriver(),
		},
		volumes: make(map[string]*types.Volume),
		state:   NewStateManager(),
	}
	req := &volume.CreateRequest{
		Name: "vol",
		Options: map[string]string{
			"type": "efs",
		},
	}
	createMountPath = func(path string) error {
		return nil
	}
	saveStateToDisk = func(b []byte) error {
		return nil
	}
	defer func() {
		createMountPath = createMountDir
		saveStateToDisk = saveState
	}()
	err := plugin.Create(req)
	assert.NoError(t, err, "create volume should be successful")
	assert.Len(t, plugin.volumes, 1)
	vol, ok := plugin.volumes["vol"]
	assert.True(t, ok)
	assert.Equal(t, "efs", vol.Type)
	assert.Equal(t, VolumeMountPathPrefix+"vol", vol.Path)
	assert.NotEmpty(t, vol.CreatedAt)
	assert.Len(t, plugin.state.VolState.Volumes, 1)
	volInfo, ok := plugin.state.VolState.Volumes["vol"]
	assert.True(t, ok)
	assert.Equal(t, "efs", volInfo.Type)
	assert.Equal(t, VolumeMountPathPrefix+"vol", volInfo.Path)
	assert.Equal(t, vol.CreatedAt, volInfo.CreatedAt)
}

func TestVolumeCreateTargetSpecified(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": NewTestVolumeDriver(),
		},
		volumes: make(map[string]*types.Volume),
		state:   NewStateManager(),
	}
	req := &volume.CreateRequest{
		Name: "vol",
		Options: map[string]string{
			"type":   "efs",
			"target": "/foo",
		},
	}
	saveStateToDisk = func(b []byte) error {
		return nil
	}
	defer func() {
		saveStateToDisk = saveState
	}()
	err := plugin.Create(req)
	assert.NoError(t, err, "create volume should be successful")
	assert.Len(t, plugin.volumes, 1)
	vol, ok := plugin.volumes["vol"]
	assert.True(t, ok)
	assert.Equal(t, "efs", vol.Type)
	assert.Equal(t, "/foo", vol.Path)
	assert.Len(t, plugin.state.VolState.Volumes, 1)
	volInfo, ok := plugin.state.VolState.Volumes["vol"]
	assert.True(t, ok)
	assert.Equal(t, "efs", volInfo.Type)
	assert.Equal(t, "/foo", volInfo.Path)
}

func TestVolumeCreateSaveFailure(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": NewTestVolumeDriver(),
		},
		volumes: make(map[string]*types.Volume),
		state:   NewStateManager(),
	}
	req := &volume.CreateRequest{
		Name: "vol",
		Options: map[string]string{
			"type": "efs",
		},
	}
	createMountPath = func(path string) error {
		return nil
	}
	saveStateToDisk = func(b []byte) error {
		return errors.New("save to disk failure")
	}
	defer func() {
		createMountPath = createMountDir
		saveStateToDisk = saveState
	}()
	err := plugin.Create(req)
	assert.NoError(t, err, "create volume successful but save state failed")
	assert.Len(t, plugin.volumes, 1)
	vol, ok := plugin.volumes["vol"]
	assert.True(t, ok)
	assert.Equal(t, "efs", vol.Type)
	assert.Equal(t, VolumeMountPathPrefix+"vol", vol.Path)
	assert.Len(t, plugin.state.VolState.Volumes, 1)
	volInfo, ok := plugin.state.VolState.Volumes["vol"]
	assert.True(t, ok)
	assert.Equal(t, "efs", volInfo.Type)
	assert.Equal(t, VolumeMountPathPrefix+"vol", volInfo.Path)
}

func TestVolumeCreateFailure(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": NewTestVolumeDriverError(),
		},
		volumes: make(map[string]*types.Volume),
	}
	req := &volume.CreateRequest{
		Name: "vol",
		Options: map[string]string{
			"type": "unknown",
		},
	}
	assert.Error(t, plugin.Create(req), "expected error while creating volume")
	assert.Len(t, plugin.volumes, 0)
}

func TestCreateNoVolumeType(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": NewTestVolumeDriver(),
		},
		volumes: make(map[string]*types.Volume),
		state:   NewStateManager(),
	}
	req := &volume.CreateRequest{
		Name: "vol",
	}
	createMountPath = func(path string) error {
		return nil
	}
	saveStateToDisk = func(b []byte) error {
		return nil
	}
	defer func() {
		createMountPath = createMountDir
		saveStateToDisk = saveState
	}()
	assert.NoError(t, plugin.Create(req), "expected no create error when no volume type specified")
	assert.Len(t, plugin.volumes, 1)
}

func TestCreateNoDriverFailure(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{},
		volumes:       make(map[string]*types.Volume),
	}
	req := &volume.CreateRequest{
		Name: "vol",
		Options: map[string]string{
			"type": "efs",
		},
	}
	assert.Error(t, plugin.Create(req), "expected create error when no corresponding volume driver present")
	assert.Len(t, plugin.volumes, 0)
}

func TestCreateMountCreationFailure(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": NewTestVolumeDriverError(),
		},
		volumes: make(map[string]*types.Volume),
	}
	req := &volume.CreateRequest{
		Name: "vol",
		Options: map[string]string{
			"type": "efs",
		},
	}
	createMountPath = func(path string) error {
		return errors.New("cannot create mount path")
	}
	defer func() {
		createMountPath = createMountDir
	}()
	assert.Error(t, plugin.Create(req), "expected create error when mount path cannot be created")
	assert.Len(t, plugin.volumes, 0)
}

func TestGetMountPathSuccess(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{},
		volumes:       make(map[string]*types.Volume),
	}
	createMountPath = func(path string) error {
		return nil
	}
	defer func() {
		createMountPath = createMountDir
	}()
	path, err := plugin.GetMountPath("vol")
	assert.NoError(t, err)
	assert.Equal(t, VolumeMountPathPrefix+"vol", path)
}

func TestGetMountPathFailure(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{},
		volumes:       make(map[string]*types.Volume),
	}
	createMountPath = func(path string) error {
		return errors.New("cannot create mount path")
	}
	defer func() {
		createMountPath = createMountDir
	}()
	path, err := plugin.GetMountPath("vol")
	assert.Error(t, err, "expected error when mount path cannot be created")
	assert.Empty(t, path)
}

func TestCleanMountPathSuccess(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{},
		volumes:       make(map[string]*types.Volume),
	}
	removeMountPath = func(path string) error {
		return nil
	}
	defer func() {
		removeMountPath = deleteMountPath
	}()
	assert.NoError(t, plugin.CleanupMountPath("vol"))
}

func TestCleanMountPathFailure(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{},
		volumes:       make(map[string]*types.Volume),
	}
	removeMountPath = func(path string) error {
		return errors.New("cannot remove dir")
	}
	defer func() {
		removeMountPath = deleteMountPath
	}()
	assert.Error(t, plugin.CleanupMountPath("vol"), "expected error when host mount path cannot be removed")
}

func TestListVolumes(t *testing.T) {
	vol := &types.Volume{}
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{},
		volumes: map[string]*types.Volume{
			"vol":  vol,
			"vol1": vol,
			"vol2": vol,
		},
	}
	resp, err := plugin.List()
	assert.NoError(t, err)
	assert.Equal(t, 3, len(resp.Volumes))
}

func TestGetVolume(t *testing.T) {
	vol := &types.Volume{
		Path:      "/var/lib/ecs/volume/vol",
		CreatedAt: "2020-01-17T21:20:04Z",
	}
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{},
		volumes: map[string]*types.Volume{
			"vol":  vol,
			"vol1": vol,
			"vol2": vol,
		},
	}
	req := &volume.GetRequest{Name: "vol1"}
	resp, err := plugin.Get(req)
	assert.NoError(t, err)
	assert.Equal(t, "vol1", resp.Volume.Name)
	assert.Equal(t, vol.Path, resp.Volume.Mountpoint)
	assert.Equal(t, vol.CreatedAt, resp.Volume.CreatedAt)
}

func TestGetVolumeError(t *testing.T) {
	vol := &types.Volume{}
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{},
		volumes: map[string]*types.Volume{
			"vol":  vol,
			"vol1": vol,
			"vol2": vol,
		},
	}
	req := &volume.GetRequest{Name: "vol4"}
	_, err := plugin.Get(req)
	assert.Error(t, err, "expected error when volume info is not found")
}

func TestVolumePath(t *testing.T) {
	volName := "vol1"
	path := VolumeMountPathPrefix + volName
	vol1 := &types.Volume{
		Path: path,
	}
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{},
		volumes: map[string]*types.Volume{
			"vol":   {},
			volName: vol1,
			"vol2":  {},
		},
	}
	req := &volume.PathRequest{Name: volName}
	resp, err := plugin.Path(req)
	assert.NoError(t, err)
	assert.Equal(t, path, resp.Mountpoint)
}

func TestVolumePathError(t *testing.T) {
	vol := &types.Volume{}
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{},
		volumes: map[string]*types.Volume{
			"vol":  vol,
			"vol1": vol,
			"vol2": vol,
		},
	}
	req := &volume.PathRequest{Name: "vol4"}
	_, err := plugin.Path(req)
	assert.Error(t, err, "expected error when volume info is not found")
}

func TestCapabilities(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{}
	resp := plugin.Capabilities()
	assert.Nil(t, resp)
}

func TestPluginLoadState(t *testing.T) {
	tcs := []struct {
		name              string
		stateFileContents string
		pluginAssertions  func(*testing.T, *AmazonECSVolumePlugin)
	}{
		{
			name: "backwards compatibility with state format without reference counting of mounts",
			stateFileContents: `
            {
                "volumes": {
                    "efsVolume": {
                        "type":"efs",
                        "path":"/var/lib/ecs/volumes/efsVolume",
                        "options": {"device":"fs-123","o":"tls","type":"efs"},
                        "mounts": {"id1": null}
                    }
                }
            }`,
			pluginAssertions: func(t *testing.T, plugin *AmazonECSVolumePlugin) {
				assert.Len(t, plugin.volumes, 1)
				vol, ok := plugin.volumes["efsVolume"]
				assert.True(t, ok)
				assert.Equal(t, "efs", vol.Type)
				assert.Equal(t, VolumeMountPathPrefix+"efsVolume", vol.Path)
				vols := plugin.state.VolState.Volumes
				assert.Len(t, vols, 1)
				volInfo, ok := vols["efsVolume"]
				require.True(t, ok)
				assert.Equal(t, "efs", volInfo.Type)
				assert.Equal(t, VolumeMountPathPrefix+"efsVolume", volInfo.Path)

				// Test for backwards compatibility of old state format following implementation of
				// reference counting of volume mounts null value for mount IDs should be converted to 1.
				assert.Equal(t, map[string]int{"id1": 1}, vols["efsVolume"].Mounts)
			},
		},
		{
			name: "current state format",
			stateFileContents: `
            {
                "volumes": {
                    "efsVolume": {
                        "type":"efs",
                        "path":"/var/lib/ecs/volumes/efsVolume",
                        "options": {"device":"fs-123","o":"tls","type":"efs"},
                        "mounts": {"id1": 1, "id2": 2}
                    }
                }
            }`,
			pluginAssertions: func(t *testing.T, plugin *AmazonECSVolumePlugin) {
				assert.Len(t, plugin.volumes, 1)
				vol, ok := plugin.volumes["efsVolume"]
				assert.True(t, ok)
				assert.Equal(t, "efs", vol.Type)
				assert.Equal(t, VolumeMountPathPrefix+"efsVolume", vol.Path)
				vols := plugin.state.VolState.Volumes
				assert.Len(t, vols, 1)
				volInfo, ok := vols["efsVolume"]
				require.True(t, ok)
				assert.Equal(t, "efs", volInfo.Type)
				assert.Equal(t, VolumeMountPathPrefix+"efsVolume", volInfo.Path)
				assert.Equal(t, map[string]int{"id1": 1, "id2": 2}, vols["efsVolume"].Mounts)
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			plugin := &AmazonECSVolumePlugin{
				volumeDrivers: map[string]driver.VolumeDriver{
					"efs": NewECSVolumeDriver(),
				},
				volumes: make(map[string]*types.Volume),
				state:   NewStateManager(),
			}
			fileExists = func(path string) bool {
				return true
			}
			readStateFile = func() ([]byte, error) {
				return []byte(tc.stateFileContents), nil
			}
			defer func() {
				fileExists = checkFile
				readStateFile = readFile
			}()
			assert.NoError(t, plugin.LoadState(), "expected no error when loading state")
			tc.pluginAssertions(t, plugin)
		})
	}
}

func TestPluginNoStateFile(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		state: NewStateManager(),
	}
	fileExists = func(path string) bool {
		return false
	}
	defer func() {
		fileExists = checkFile
	}()
	assert.NoError(t, plugin.LoadState())
}

func TestPluginInvalidState(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		state: NewStateManager(),
	}
	fileExists = func(path string) bool {
		return true
	}
	readStateFile = func() ([]byte, error) {
		return []byte(`{"junk"}`), nil
	}
	defer func() {
		fileExists = checkFile
		readStateFile = readFile
	}()
	assert.Error(t, plugin.LoadState(), "expected error when loading invalid state")
}

func TestPluginEmptyState(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": NewTestVolumeDriver(),
		},
		volumes: make(map[string]*types.Volume),
		state:   NewStateManager(),
	}
	fileExists = func(path string) bool {
		return true
	}
	readStateFile = func() ([]byte, error) {
		return []byte(`{}`), nil
	}
	defer func() {
		fileExists = checkFile
		readStateFile = readFile
	}()
	assert.NoError(t, plugin.LoadState(), "expected no error when loading empty state")
	assert.Len(t, plugin.volumes, 0)
	req := &volume.CreateRequest{
		Name: "vol",
		Options: map[string]string{
			"type": "efs",
		},
	}
	createMountPath = func(path string) error {
		return nil
	}
	saveStateToDisk = func(b []byte) error {
		return nil
	}
	defer func() {
		createMountPath = createMountDir
		saveStateToDisk = saveState
	}()
	err := plugin.Create(req)
	assert.NoError(t, err, "create volume should be successful after loading empty state")
	assert.Len(t, plugin.volumes, 1)
	vol, ok := plugin.volumes["vol"]
	assert.True(t, ok)
	assert.Equal(t, "efs", vol.Type)
	assert.Equal(t, VolumeMountPathPrefix+"vol", vol.Path)
	assert.Len(t, plugin.state.VolState.Volumes, 1)
	volInfo, ok := plugin.state.VolState.Volumes["vol"]
	assert.True(t, ok)
	assert.Equal(t, "efs", volInfo.Type)
	assert.Equal(t, VolumeMountPathPrefix+"vol", volInfo.Path)
}

func TestGetVolumeDriver_S3Files(t *testing.T) {
	plugin := NewAmazonECSVolumePlugin()
	driver, err := plugin.getVolumeDriver("s3files")
	assert.NoError(t, err)
	assert.NotNil(t, driver)
}

func TestCreate_S3FilesVolume(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"s3files": NewTestVolumeDriver(),
		},
		volumes: make(map[string]*types.Volume),
		state:   NewStateManager(),
	}
	req := &volume.CreateRequest{
		Name: "s3vol",
		Options: map[string]string{
			"type":   "s3files",
			"device": "fs-123",
			"o":      "tls,iam",
		},
	}
	createMountPath = func(path string) error {
		return nil
	}
	saveStateToDisk = func(b []byte) error {
		return nil
	}
	defer func() {
		createMountPath = createMountDir
		saveStateToDisk = saveState
	}()
	err := plugin.Create(req)
	assert.NoError(t, err, "create s3files volume should be successful")
	assert.Len(t, plugin.volumes, 1)
	vol, ok := plugin.volumes["s3vol"]
	assert.True(t, ok)
	assert.Equal(t, "s3files", vol.Type)
	assert.Equal(t, VolumeMountPathPrefix+"s3vol", vol.Path)
	assert.NotEmpty(t, vol.CreatedAt)
}
