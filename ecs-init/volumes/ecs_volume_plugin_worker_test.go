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
	"fmt"
	"sync"
	"testing"

	"github.com/aws/amazon-ecs-agent/ecs-init/volumes/driver"
	mock_driver "github.com/aws/amazon-ecs-agent/ecs-init/volumes/driver/mock"
	"github.com/aws/amazon-ecs-agent/ecs-init/volumes/types"
	"github.com/docker/go-plugins-helpers/volume"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestVolumeRemoveHappyPath(t *testing.T) {
	volName := "vol"
	path := VolumeMountPathPrefix + volName
	vol := &types.Volume{
		Path: path,
		Type: "efs",
	}
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": NewTestVolumeDriver(),
		},
		volumes: map[string]*types.Volume{
			volName: vol,
		},
		state:   NewStateManager(),
		workers: make(map[string]*volumeWorker),
	}
	req := &volume.RemoveRequest{Name: volName}
	removeMountPath = func(path string) error {
		return nil
	}
	saveStateToDisk = func(b []byte) error {
		return nil
	}
	defer func() {
		removeMountPath = deleteMountPath
		saveStateToDisk = saveState
	}()
	assert.NoError(t, plugin.Remove(req))
	assert.Len(t, plugin.volumes, 0)
	assert.Len(t, plugin.state.VolState.Volumes, 0)
}

func TestVolumeRemoveFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	volName := "vol"
	path := VolumeMountPathPrefix + volName
	vol := &types.Volume{
		Path: path,
		Type: "efs",
	}
	efsDriver := mock_driver.NewMockVolumeDriver(ctrl)
	efsDriver.EXPECT().IsMounted(volName).Return(true)
	efsDriver.EXPECT().Remove(&driver.RemoveRequest{Name: volName}).Return(errors.New("error"))
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": efsDriver,
		},
		volumes: map[string]*types.Volume{
			volName: vol,
		},
		state:   NewStateManager(),
		workers: make(map[string]*volumeWorker),
	}
	saveStateToDisk = func(b []byte) error {
		return nil
	}
	defer func() {
		saveStateToDisk = saveState
	}()
	req := &volume.RemoveRequest{Name: volName}
	assert.Error(t, plugin.Remove(req), "expected error when remove volume fails")
	assert.Len(t, plugin.volumes, 1)
}

func TestRemoveVolumeNotFound(t *testing.T) {
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": NewTestVolumeDriver(),
		},
		volumes: map[string]*types.Volume{},
		state:   NewStateManager(),
		workers: make(map[string]*volumeWorker),
	}
	req := &volume.RemoveRequest{Name: "vol"}
	assert.Error(t, plugin.Remove(req), "expected error when volume to remove is not found")
}

func TestRemoveVolumeDriverNotFound(t *testing.T) {
	volName := "vol"
	path := VolumeMountPathPrefix + volName
	vol := &types.Volume{
		Path: path,
		Type: "efs",
	}
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"xyz": NewTestVolumeDriver(),
		},
		volumes: map[string]*types.Volume{
			volName: vol,
		},
		state:   NewStateManager(),
		workers: make(map[string]*volumeWorker),
	}
	req := &volume.RemoveRequest{Name: volName}
	assert.Error(t, plugin.Remove(req), "expected error when corresponding volume driver not found")
}

// Tests that Remove method does not attempt to unmount a volume that's already unmounted.
func TestVolumeRemoveNoUnmountIfAlreadyUnmounted(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	volName := "vol"
	path := VolumeMountPathPrefix + volName
	vol := &types.Volume{
		Path: path,
		Type: "efs",
	}
	efsDriver := mock_driver.NewMockVolumeDriver(ctrl)
	efsDriver.EXPECT().IsMounted(volName).Return(false) // Not mounted
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": efsDriver,
		},
		volumes: map[string]*types.Volume{
			volName: vol,
		},
		state:   NewStateManager(),
		workers: make(map[string]*volumeWorker),
	}
	saveStateToDisk = func(b []byte) error {
		return nil
	}
	defer func() {
		saveStateToDisk = saveState
	}()
	req := &volume.RemoveRequest{Name: volName}
	assert.NoError(t, plugin.Remove(req))
	assert.Len(t, plugin.volumes, 0)
}

func TestVolumeRemoveMountPathFailure(t *testing.T) {
	volName := "vol"
	path := VolumeMountPathPrefix + volName
	vol := &types.Volume{
		Path: path,
		Type: "efs",
	}
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": NewTestVolumeDriver(),
		},
		volumes: map[string]*types.Volume{
			volName: vol,
		},
		state:   NewStateManager(),
		workers: make(map[string]*volumeWorker),
	}
	req := &volume.RemoveRequest{Name: volName}
	removeMountPath = func(path string) error {
		return errors.New("removing path failed")
	}
	saveStateToDisk = func(b []byte) error {
		return nil
	}
	defer func() {
		removeMountPath = deleteMountPath
		saveStateToDisk = saveState
	}()
	assert.NoError(t, plugin.Remove(req))
	assert.Len(t, plugin.volumes, 0)
	assert.Len(t, plugin.state.VolState.Volumes, 0)
}

func TestVolumeRemoveStateSaveFailure(t *testing.T) {
	volName := "vol"
	path := VolumeMountPathPrefix + volName
	vol := &types.Volume{
		Path: path,
		Type: "efs",
	}
	plugin := &AmazonECSVolumePlugin{
		volumeDrivers: map[string]driver.VolumeDriver{
			"efs": NewTestVolumeDriver(),
		},
		volumes: map[string]*types.Volume{
			volName: vol,
		},
		state:   NewStateManager(),
		workers: make(map[string]*volumeWorker),
	}
	req := &volume.RemoveRequest{Name: volName}
	removeMountPath = func(path string) error {
		return nil
	}
	saveStateToDisk = func(b []byte) error {
		return errors.New("save to disk failed")
	}
	defer func() {
		removeMountPath = deleteMountPath
		saveStateToDisk = saveState
	}()
	assert.NoError(t, plugin.Remove(req))
	assert.Len(t, plugin.volumes, 0)
	assert.Len(t, plugin.state.VolState.Volumes, 0)
}

// Tests for plugin's Mount method.
func TestPluginMount(t *testing.T) {
	const (
		volName       = "volume"
		volPath       = "path"
		driverTypeEFS = "efs"
		reqMountID    = "mountID"
	)
	volOpts := map[string]string{"opt1": "opt1"}

	tcs := []struct {
		name                  string
		pluginVolumes         map[string]*types.Volume
		setDriverExpectations func(d *mock_driver.MockVolumeDriver)
		mockSaveStateFn       func(b []byte) error
		req                   *volume.MountRequest
		expectedResponse      *volume.MountResponse
		expectedError         string
		assertPluginState     func(t *testing.T, plugin *AmazonECSVolumePlugin)
	}{
		{
			name:          "volume not found",
			req:           &volume.MountRequest{Name: "unknown", ID: reqMountID},
			expectedError: "volume unknown not found",
		},
		{
			name: "first mount on the volume",
			setDriverExpectations: func(d *mock_driver.MockVolumeDriver) {
				d.EXPECT().
					Create(&driver.CreateRequest{Name: volName, Path: volPath, Options: volOpts}).
					Return(nil)
			},
			pluginVolumes:    map[string]*types.Volume{volName: {Path: volPath, Options: volOpts}},
			req:              &volume.MountRequest{Name: volName, ID: reqMountID},
			expectedResponse: &volume.MountResponse{Mountpoint: volPath},
			assertPluginState: func(t *testing.T, plugin *AmazonECSVolumePlugin) {
				mounts := map[string]int{reqMountID: 1}
				assert.Equal(t,
					map[string]*types.Volume{
						volName: {Path: volPath, Options: volOpts, Mounts: mounts},
					},
					plugin.volumes)
				assert.Equal(t,
					&VolumeState{
						Volumes: map[string]*VolumeInfo{
							volName: {Path: volPath, Options: volOpts, Mounts: mounts},
						},
					},
					plugin.state.VolState)
			},
		},
		{
			name: "volume already has mounts - no interaction with driver",
			pluginVolumes: map[string]*types.Volume{
				volName: {
					Path:   volPath,
					Mounts: map[string]int{"someMount": 1},
				},
			},
			req:              &volume.MountRequest{Name: volName, ID: reqMountID},
			expectedResponse: &volume.MountResponse{Mountpoint: volPath},
			assertPluginState: func(t *testing.T, plugin *AmazonECSVolumePlugin) {
				mounts := map[string]int{reqMountID: 1, "someMount": 1}
				assert.Equal(t,
					map[string]*types.Volume{volName: {Path: volPath, Mounts: mounts}},
					plugin.volumes)
				assert.Equal(t,
					&VolumeState{
						Volumes: map[string]*VolumeInfo{volName: {Path: volPath, Mounts: mounts}},
					},
					plugin.state.VolState)
			},
		},
		{
			name:          "invalid driver type",
			pluginVolumes: map[string]*types.Volume{volName: {Path: volPath, Type: "unknown"}},
			req:           &volume.MountRequest{Name: volName, ID: reqMountID},
			expectedError: "no volume driver found for type unknown",
		},
		{
			name:          "no ID in the request",
			req:           &volume.MountRequest{Name: volName},
			expectedError: "no mount ID in the request",
		},
		{
			name:          "no volume in the request",
			req:           &volume.MountRequest{},
			expectedError: "no volume in the request",
		},
		{
			name: "driver fails to mount",
			setDriverExpectations: func(d *mock_driver.MockVolumeDriver) {
				d.EXPECT().
					Create(&driver.CreateRequest{Name: volName, Path: volPath}).
					Return(errors.New("some error"))
			},
			pluginVolumes: map[string]*types.Volume{volName: {Path: volPath}},
			req:           &volume.MountRequest{Name: volName, ID: reqMountID},
			expectedError: "failed to mount volume volume: some error",
			assertPluginState: func(t *testing.T, plugin *AmazonECSVolumePlugin) {
				// No mounts expected on the volume
				assert.Equal(t, map[string]*types.Volume{volName: {Path: volPath}}, plugin.volumes)
				assert.Equal(t,
					&VolumeState{Volumes: map[string]*VolumeInfo{
						volName: {Path: volPath, Mounts: map[string]int{}},
					}},
					plugin.state.VolState)
			},
		},
		{
			name: "duplicate mount increments mount reference count",
			pluginVolumes: map[string]*types.Volume{
				volName: {
					Path:    volPath,
					Mounts:  map[string]int{reqMountID: 1},
					Options: volOpts,
				},
			},
			req:              &volume.MountRequest{Name: volName, ID: reqMountID},
			expectedResponse: &volume.MountResponse{Mountpoint: volPath},
			assertPluginState: func(t *testing.T, plugin *AmazonECSVolumePlugin) {
				mounts := map[string]int{reqMountID: 2}
				assert.Equal(t,
					map[string]*types.Volume{
						volName: {Path: volPath, Options: volOpts, Mounts: mounts},
					},
					plugin.volumes)
				assert.Equal(t,
					&VolumeState{
						Volumes: map[string]*VolumeInfo{
							volName: {Path: volPath, Options: volOpts, Mounts: mounts},
						},
					},
					plugin.state.VolState)
			},
		},
		{
			name: "roll back changes if saving state fails",
			setDriverExpectations: func(d *mock_driver.MockVolumeDriver) {
				d.EXPECT().Create(&driver.CreateRequest{Name: volName, Path: volPath}).Return(nil)
				d.EXPECT().Remove(&driver.RemoveRequest{Name: volName}).Return(nil) // mount rollback
			},
			pluginVolumes:   map[string]*types.Volume{volName: {Path: volPath}},
			mockSaveStateFn: func(b []byte) error { return errors.New("some error") },
			req:             &volume.MountRequest{Name: volName, ID: reqMountID},
			expectedError:   "mount failed due to an error while saving state: some error",
			assertPluginState: func(t *testing.T, plugin *AmazonECSVolumePlugin) {
				// No mounts expected on the volume
				mounts := map[string]int{}
				assert.Equal(t,
					map[string]*types.Volume{volName: {Path: volPath, Mounts: mounts}},
					plugin.volumes)
				assert.Equal(t,
					&VolumeState{
						Volumes: map[string]*VolumeInfo{volName: {Path: volPath, Mounts: mounts}},
					},
					plugin.state.VolState)
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Prepare a mock driver
			efsDriver := mock_driver.NewMockVolumeDriver(ctrl)
			if tc.setDriverExpectations != nil {
				tc.setDriverExpectations(efsDriver)
			}

			// Mock saveState function for preparing plugin for testing
			saveStateToDisk = func(b []byte) error {
				return nil
			}
			defer func() {
				saveStateToDisk = saveState
			}()

			// Prepare a plugin for testing with state loaded
			pluginState := NewStateManager()
			plugin := AmazonECSVolumePlugin{
				volumes:       tc.pluginVolumes,
				volumeDrivers: map[string]driver.VolumeDriver{driverTypeEFS: efsDriver},
				state:         pluginState,
				workers:       make(map[string]*volumeWorker),
			}
			for volName, vol := range tc.pluginVolumes {
				pluginState.recordVolume(volName, vol)
			}

			// Mock saveState function for the test case
			if tc.mockSaveStateFn != nil {
				saveStateToDisk = tc.mockSaveStateFn
			}

			// Test
			res, err := plugin.Mount(tc.req)
			if tc.expectedError == "" {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedResponse, res)
			} else {
				assert.EqualError(t, err, tc.expectedError)
			}
			if tc.assertPluginState != nil {
				tc.assertPluginState(t, &plugin)
			}
		})
	}
}

// Tests for plugin's Unmount method.
func TestPluginUnmount(t *testing.T) {
	const (
		volName       = "volume"
		volPath       = "path"
		driverTypeEFS = "efs"
		reqMountID    = "mountID"
	)
	volOpts := map[string]string{"opt1": "opt1"}

	tcs := []struct {
		name                  string
		pluginVolumes         map[string]*types.Volume
		setDriverExpectations func(d *mock_driver.MockVolumeDriver)
		mockSaveStateFn       func(b []byte) error
		req                   *volume.UnmountRequest
		expectedError         string
		assertPluginState     func(t *testing.T, plugin *AmazonECSVolumePlugin)
	}{
		{
			name:          "volume not found",
			req:           &volume.UnmountRequest{Name: "unknown", ID: reqMountID},
			expectedError: "volume unknown not found",
		},
		{
			name: "only one mount on the volume",
			setDriverExpectations: func(d *mock_driver.MockVolumeDriver) {
				d.EXPECT().Remove(&driver.RemoveRequest{Name: volName}).Return(nil)
			},
			pluginVolumes: map[string]*types.Volume{
				volName: {Path: volPath, Mounts: map[string]int{reqMountID: 1}},
			},
			req: &volume.UnmountRequest{Name: volName, ID: reqMountID},
			assertPluginState: func(t *testing.T, plugin *AmazonECSVolumePlugin) {
				mounts := map[string]int{}
				assert.Equal(t,
					map[string]*types.Volume{volName: {Path: volPath, Mounts: mounts}},
					plugin.volumes)
				assert.Equal(t,
					&VolumeState{
						Volumes: map[string]*VolumeInfo{volName: {Path: volPath, Mounts: mounts}},
					},
					plugin.state.VolState)
			},
		},
		{
			name: "more than one mount on the volume - no interaction with the driver",
			pluginVolumes: map[string]*types.Volume{
				volName: {
					Path:   volPath,
					Mounts: map[string]int{"someMount": 1, reqMountID: 1},
				},
			},
			req: &volume.UnmountRequest{Name: volName, ID: reqMountID},
			assertPluginState: func(t *testing.T, plugin *AmazonECSVolumePlugin) {
				mounts := map[string]int{"someMount": 1}
				assert.Equal(t,
					map[string]*types.Volume{volName: {Path: volPath, Mounts: mounts}},
					plugin.volumes)
				assert.Equal(t,
					&VolumeState{
						Volumes: map[string]*VolumeInfo{volName: {Path: volPath, Mounts: mounts}},
					},
					plugin.state.VolState)
			},
		},
		{
			name: "mount reference count decrements",
			pluginVolumes: map[string]*types.Volume{
				volName: {
					Path:   volPath,
					Mounts: map[string]int{reqMountID: 2},
				},
			},
			req: &volume.UnmountRequest{Name: volName, ID: reqMountID},
			assertPluginState: func(t *testing.T, plugin *AmazonECSVolumePlugin) {
				mounts := map[string]int{reqMountID: 1}
				assert.Equal(t,
					map[string]*types.Volume{volName: {Path: volPath, Mounts: mounts}},
					plugin.volumes)
				assert.Equal(t,
					&VolumeState{
						Volumes: map[string]*VolumeInfo{volName: {Path: volPath, Mounts: mounts}},
					},
					plugin.state.VolState)
			},
		},
		{
			name:          "invalid driver type",
			pluginVolumes: map[string]*types.Volume{volName: {Path: volPath, Type: "unknown"}},
			req:           &volume.UnmountRequest{Name: volName, ID: reqMountID},
			expectedError: "no corresponding volume driver found for type unknown",
		},
		{
			name:          "no ID in the request",
			req:           &volume.UnmountRequest{Name: volName},
			expectedError: "no mount ID in the request",
		},
		{
			name:          "no volume in the request",
			req:           &volume.UnmountRequest{},
			expectedError: "no volume in the request",
		},
		{
			name: "driver fails to unmount",
			setDriverExpectations: func(d *mock_driver.MockVolumeDriver) {
				d.EXPECT().
					Remove(&driver.RemoveRequest{Name: volName}).
					Return(errors.New("some error"))
			},
			pluginVolumes: map[string]*types.Volume{
				volName: {Path: volPath, Mounts: map[string]int{reqMountID: 1}},
			},
			req:           &volume.UnmountRequest{Name: volName, ID: reqMountID},
			expectedError: "failed to unmount volume volume: some error",
			assertPluginState: func(t *testing.T, plugin *AmazonECSVolumePlugin) {
				// Mount should not exist in the plugin state
				mounts := map[string]int{}
				assert.Equal(t,
					map[string]*types.Volume{volName: {Path: volPath, Mounts: mounts}},
					plugin.volumes)
			},
		},
		{
			name:          "no-op when mount not found on the volume",
			pluginVolumes: map[string]*types.Volume{volName: {Path: volPath, Options: volOpts}},
			req:           &volume.UnmountRequest{Name: volName, ID: reqMountID},
			assertPluginState: func(t *testing.T, plugin *AmazonECSVolumePlugin) {
				mounts := map[string]int{}
				assert.Equal(t,
					map[string]*types.Volume{
						volName: {Path: volPath, Mounts: nil, Options: volOpts},
					},
					plugin.volumes)
				assert.Equal(t,
					&VolumeState{
						Volumes: map[string]*VolumeInfo{
							volName: {Path: volPath, Mounts: mounts, Options: volOpts},
						},
					},
					plugin.state.VolState)
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			// Prepare a mock driver
			efsDriver := mock_driver.NewMockVolumeDriver(ctrl)
			if tc.setDriverExpectations != nil {
				tc.setDriverExpectations(efsDriver)
			}

			// Mock saveState function for preparing plugin for testing
			saveStateToDisk = func(b []byte) error {
				return nil
			}
			defer func() {
				saveStateToDisk = saveState
			}()

			// Prepare a plugin for testing with state loaded
			pluginState := NewStateManager()
			plugin := AmazonECSVolumePlugin{
				volumes:       tc.pluginVolumes,
				volumeDrivers: map[string]driver.VolumeDriver{driverTypeEFS: efsDriver},
				state:         pluginState,
				workers:       make(map[string]*volumeWorker),
			}
			for volName, vol := range tc.pluginVolumes {
				pluginState.recordVolume(volName, vol)
			}

			// Mock saveState function from the test case
			if tc.mockSaveStateFn != nil {
				saveStateToDisk = tc.mockSaveStateFn
			}

			// Test
			err := plugin.Unmount(tc.req)
			if tc.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tc.expectedError)
			}
			if tc.assertPluginState != nil {
				tc.assertPluginState(t, &plugin)
			}
		})
	}
}

// TestConcurrentMountsDifferentVolumes verifies that mount operations for different
// volumes can proceed concurrently rather than being serialized behind a single lock.
// Uses a barrier (sync.WaitGroup) instead of wall-clock timing to prove concurrency.
func TestConcurrentMountsDifferentVolumes(t *testing.T) {
	const numVolumes = 5

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	saveStateToDisk = func(b []byte) error { return nil }
	defer func() { saveStateToDisk = saveState }()

	// Barrier: each Create() signals arrival, then waits for all to arrive.
	// If mounts were serialized, the second goroutine would never enter Create()
	// until the first returns — so the barrier would deadlock (caught by test timeout).
	var entered sync.WaitGroup
	entered.Add(numVolumes)

	mockDriver := mock_driver.NewMockVolumeDriver(ctrl)
	mockDriver.EXPECT().
		Create(gomock.Any()).
		DoAndReturn(func(r *driver.CreateRequest) error {
			entered.Done()
			entered.Wait()
			return nil
		}).Times(numVolumes)

	volumes := make(map[string]*types.Volume)
	for i := 0; i < numVolumes; i++ {
		volumes[fmt.Sprintf("vol%d", i)] = &types.Volume{
			Path:    fmt.Sprintf("/mnt/vol%d", i),
			Mounts:  map[string]int{},
			Options: map[string]string{},
		}
	}

	pluginState := NewStateManager()
	plugin := &AmazonECSVolumePlugin{
		volumes:       volumes,
		volumeDrivers: map[string]driver.VolumeDriver{"efs": mockDriver},
		state:         pluginState,
		workers:       make(map[string]*volumeWorker),
	}
	for volName, vol := range volumes {
		pluginState.recordVolume(volName, vol)
	}

	var wg sync.WaitGroup
	errs := make([]error, numVolumes)
	for i := 0; i < numVolumes; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = plugin.Mount(&volume.MountRequest{
				Name: fmt.Sprintf("vol%d", idx),
				ID:   fmt.Sprintf("mount%d", idx),
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "mount vol%d should succeed", i)
	}
}

// TestConcurrentMountsSameVolume verifies that concurrent Mount() calls for the
// same volume result in exactly one driver Create() call.
func TestConcurrentMountsSameVolume(t *testing.T) {
	const (
		volName   = "shared-vol"
		numMounts = 5
	)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	saveStateToDisk = func(b []byte) error { return nil }
	defer func() { saveStateToDisk = saveState }()

	mockDriver := mock_driver.NewMockVolumeDriver(ctrl)
	mockDriver.EXPECT().
		Create(&driver.CreateRequest{
			Name:    volName,
			Path:    "/mnt/shared",
			Options: map[string]string{},
		}).
		Return(nil).
		Times(1)

	pluginState := NewStateManager()
	vol := &types.Volume{
		Path:    "/mnt/shared",
		Mounts:  map[string]int{},
		Options: map[string]string{},
	}
	plugin := &AmazonECSVolumePlugin{
		volumes:       map[string]*types.Volume{volName: vol},
		volumeDrivers: map[string]driver.VolumeDriver{"efs": mockDriver},
		state:         pluginState,
		workers:       make(map[string]*volumeWorker),
	}
	pluginState.recordVolume(volName, vol)

	var wg sync.WaitGroup
	errs := make([]error, numMounts)
	for i := 0; i < numMounts; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = plugin.Mount(&volume.MountRequest{
				Name: volName,
				ID:   fmt.Sprintf("mount%d", idx),
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "mount %d should succeed", i)
	}
	assert.Equal(t, numMounts, len(vol.Mounts), "all mounts should be recorded")
}
