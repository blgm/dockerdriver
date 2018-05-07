package driverhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/voldriver"
	"code.cloudfoundry.org/volman"
)

type DockerDriverPlugin struct {
	DockerDriver interface{}
}

func NewDockerPluginWithDriver(driver voldriver.Driver) volman.Plugin {
	return &DockerDriverPlugin{
		DockerDriver: driver,
	}
}

func (dw *DockerDriverPlugin) Matches(logger lager.Logger, pluginSpec volman.PluginSpec) bool {
	logger = logger.Session("matches")
	logger.Info("start")
	defer logger.Info("end")

	var matches bool
	matchableDriver, ok := dw.DockerDriver.(voldriver.MatchableDriver)
	logger.Info("matches", lager.Data{"is-matchable": ok})
	if ok {
		var tlsConfig *voldriver.TLSConfig
		if pluginSpec.TLSConfig != nil {
			tlsConfig = &voldriver.TLSConfig{
				InsecureSkipVerify: pluginSpec.TLSConfig.InsecureSkipVerify,
				CAFile:             pluginSpec.TLSConfig.CAFile,
				CertFile:           pluginSpec.TLSConfig.CertFile,
				KeyFile:            pluginSpec.TLSConfig.KeyFile,
			}
		}
		matches = matchableDriver.Matches(logger, pluginSpec.Address, tlsConfig)
	}
	logger.Info("matches", lager.Data{"matches": matches})
	return matches
}

func (d *DockerDriverPlugin) ListVolumes(logger lager.Logger) ([]string, error) {
	logger = logger.Session("list-volumes")
	logger.Info("start")
	defer logger.Info("end")

	volumes := []string{}
	env := NewHttpDriverEnv(logger, context.TODO())

	response := d.DockerDriver.(voldriver.Driver).List(env)
	if response.Err != "" {
		return volumes, errors.New(response.Err)
	}

	for _, volumeInfo := range response.Volumes {
		volumes = append(volumes, volumeInfo.Name)
	}

	return volumes, nil
}

func (d *DockerDriverPlugin) Mount(logger lager.Logger, volumeId string, opts map[string]interface{}) (volman.MountResponse, error) {
	logger = logger.Session("mount")
	logger.Info("start")
	defer logger.Info("end")

	env := NewHttpDriverEnv(logger, context.TODO())

	logger.Debug("creating-volume", lager.Data{"volumeId": volumeId})
	response := d.DockerDriver.(voldriver.Driver).Create(env, voldriver.CreateRequest{Name: volumeId, Opts: opts})
	if response.Err != "" {
		return volman.MountResponse{}, errors.New(response.Err)
	}

	mountRequest := voldriver.MountRequest{Name: volumeId}
	logger.Debug("calling-docker-driver-with-mount-request", lager.Data{"mountRequest": mountRequest})
	mountResponse := d.DockerDriver.(voldriver.Driver).Mount(env, mountRequest)
	logger.Debug("response-from-docker-driver", lager.Data{"response": mountResponse})

	if !strings.HasPrefix(mountResponse.Mountpoint, "/var/vcap/data") {
		logger.Info("invalid-mountpath", lager.Data{"detail": fmt.Sprintf("Invalid or dangerous mountpath %s outside of /var/vcap/data", mountResponse.Mountpoint)})
	}

	if mountResponse.Err != "" {
		safeError := voldriver.SafeError{}
		err := json.Unmarshal([]byte(mountResponse.Err), &safeError)
		if err == nil {
			return volman.MountResponse{}, safeError
		} else {
			return volman.MountResponse{}, errors.New(mountResponse.Err)
		}
	}

	return volman.MountResponse{Path: mountResponse.Mountpoint}, nil
}

func (d *DockerDriverPlugin) Unmount(logger lager.Logger, volumeId string) error {
	logger = logger.Session("unmount")
	logger.Info("start")
	defer logger.Info("end")

	env := NewHttpDriverEnv(logger, context.TODO())

	if response := d.DockerDriver.(voldriver.Driver).Unmount(env, voldriver.UnmountRequest{Name: volumeId}); response.Err != "" {

		safeError := voldriver.SafeError{}
		err := json.Unmarshal([]byte(response.Err), &safeError)
		if err == nil {
			err = safeError
		} else {
			err = errors.New(response.Err)
		}

		logger.Error("unmount-failed", err)
		return err
	}
	return nil
}
