package engines

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/camptocamp/bivac/metrics"
	"github.com/camptocamp/bivac/orchestrators"
	"github.com/camptocamp/bivac/util"
	"github.com/camptocamp/bivac/volume"
)

// ResticEngine implements a backup engine with Restic
type ResticEngine struct {
	Orchestrator orchestrators.Orchestrator
	Volume       *volume.Volume
}

// Snapshot is a struct returned by the function snapshots()
type Snapshot struct {
	Time     time.Time `json:"time"`
	Parent   string    `json:"parent"`
	Tree     string    `json:"tree"`
	Path     []string  `json:"path"`
	Hostname string    `json:"hostname"`
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
}

// GetName returns the engine name
func (*ResticEngine) GetName() string {
	return "Restic"
}

// Backup performs the backup of the passed volume
func (r *ResticEngine) Backup() (err error) {

	v := r.Volume

	targetURL, err := url.Parse(v.Config.TargetURL)
	if err != nil {
		err = fmt.Errorf("failed to parse target URL: %v", err)
		return
	}

	c := r.Orchestrator.GetHandler()
	v.Target = targetURL.String() + "/" + v.Hostname + "/" + v.Name
	v.BackupDir = v.Mountpoint + "/" + v.BackupDir
	v.Mount = v.Name + ":" + v.Mountpoint + ":ro"

	err = util.Retry(3, r.init)
	if err != nil {
		err = fmt.Errorf("failed to create a secure repository: %v", err)
		return
	}

	err = util.Retry(3, r.resticBackup)
	if err != nil {
		err = fmt.Errorf("failed to backup the volume: %v", err)
		return
	}

	err = util.Retry(3, r.forget)
	if err != nil {
		err = fmt.Errorf("failed to forget the oldest snapshots: %v", err)
		return
	}

	if _, err := c.IsCheckScheduled(v); err == nil {
		err = util.Retry(3, r.verify)
		if err != nil {
			err = fmt.Errorf("failed to verify backup: %v", err)
			return err
		}
	}

	return
}

// init initialize a secure repository
func (r *ResticEngine) init() (err error) {
	v := r.Volume

	// Check if the repository already exists
	state, _, err := r.launchRestic(
		[]string{
			"-r",
			v.Target,
			"snapshots",
		},
		[]*volume.Volume{},
	)
	if err != nil {
		err = fmt.Errorf("failed to launch Restic to verify the existence of the repository: %v", err)
		return
	}
	if state == 0 {
		log.WithFields(log.Fields{
			"volume": v.Name,
		}).Info("The repository already exists, skipping initialization.")
		return nil
	}

	// Initialize the repository
	state, _, err = r.launchRestic(
		[]string{
			"-r",
			v.Target,
			"init",
		},
		[]*volume.Volume{
			v,
		},
	)
	if err != nil {
		err = fmt.Errorf("failed to launch Restic to initialize the repository: %v", err)
		return
	}
	if state != 0 {
		err = fmt.Errorf("Restic exited with state %v while initializing the repository", state)
		return
	}
	return
}

// resticBackup performs the backup of a volume with Restic
func (r *ResticEngine) resticBackup() (err error) {
	c := r.Orchestrator.GetHandler()
	v := r.Volume
	state, _, err := r.launchRestic(
		[]string{
			"--hostname",
			c.Hostname,
			"-r",
			v.Target,
			"backup",
			v.BackupDir,
		},
		[]*volume.Volume{
			v,
		},
	)
	if err != nil {
		err = fmt.Errorf("failed to launch Restic to backup the volume: %v", err)
	}
	if state != 0 {
		err = fmt.Errorf("Restic exited with state %v while backuping the volume", state)
	}

	metric := r.Volume.MetricsHandler.NewMetric("bivac_backupExitCode", "gauge")
	metric.UpdateEvent(
		&metrics.Event{
			Labels: map[string]string{
				"volume": v.Name,
			},
			Value: strconv.Itoa(state),
		},
	)
	return
}

// verify checks that the backup is usable
func (r *ResticEngine) verify() (err error) {
	v := r.Volume
	state, _, err := r.launchRestic(
		[]string{
			"-r",
			v.Target,
			"check",
		},
		[]*volume.Volume{},
	)
	if err != nil {
		err = fmt.Errorf("failed to launch Restic to check the backup: %v", err)
		return
	}
	if state == 0 {
		now := time.Now().Local()
		os.Chtimes(v.Mountpoint+"/.bivac_last_check", now, now)
	} else {
		err = fmt.Errorf("Restic exited with state %v while checking the backup", state)
	}

	metric := r.Volume.MetricsHandler.NewMetric("bivac_verifyExitCode", "gauge")
	err = metric.UpdateEvent(
		&metrics.Event{
			Labels: map[string]string{
				"volume": v.Name,
			},
			Value: strconv.Itoa(state),
		},
	)
	return
}

// forget removes a snapshot
func (r *ResticEngine) forget() (err error) {

	v := r.Volume

	snapshots, err := r.snapshots()

	duration, err := util.GetDurationFromInterval(v.Config.RemoveOlderThan)
	if err != nil {
		return err
	}

	validSnapshots := 0
	now := time.Now()
	for _, snapshot := range snapshots {
		expiration := snapshot.Time.Add(duration)
		if now.Before(expiration) {
			validSnapshots++
		}
	}

	state, output, err := r.launchRestic(
		[]string{
			"-r",
			v.Target,
			"forget",
			"--prune",
			"--keep-last",
			fmt.Sprintf("%d", validSnapshots),
		},
		[]*volume.Volume{},
	)
	if err != nil {
		err = fmt.Errorf("failed to launch Restic to forget the snapshot: %v", err)
		return err
	}

	if state != 0 {
		err = fmt.Errorf("restic failed to forget old snapshots: %v", output)
		return err
	}
	return
}

// snapshots lists snapshots
func (r *ResticEngine) snapshots() (snapshots []Snapshot, err error) {
	v := r.Volume

	_, output, err := r.launchRestic(
		[]string{
			"-r",
			v.Target,
			"snapshots",
			"--json",
		},
		[]*volume.Volume{},
	)
	if err != nil {
		err = fmt.Errorf("failed to launch Restic to check the backup: %v", err)
		return
	}

	if err := json.Unmarshal([]byte(output), &snapshots); err != nil {
		err = fmt.Errorf("failed to parse JSON output: %v", err)
		return snapshots, err
	}
	return
}

// launchRestic starts a restic container with the given command
func (r *ResticEngine) launchRestic(cmd []string, volumes []*volume.Volume) (state int, stdout string, err error) {
	config := r.Orchestrator.GetHandler().Config
	image := config.Restic.Image

	// Disable cache to avoid volume issues with Kubernetes
	cmd = append(cmd, "--no-cache")

	env := map[string]string{
		"AWS_ACCESS_KEY_ID":     config.AWS.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY": config.AWS.SecretAccessKey,
		"OS_USERNAME":           config.Swift.Username,
		"OS_PASSWORD":           config.Swift.Password,
		"OS_AUTH_URL":           config.Swift.AuthURL,
		"OS_TENANT_NAME":        config.Swift.TenantName,
		"OS_REGION_NAME":        config.Swift.RegionName,
		"RESTIC_PASSWORD":       config.Restic.Password,
	}

	for k, v := range config.ExtraEnv {
		env[k] = v
	}

	return r.Orchestrator.LaunchContainer(image, env, cmd, volumes)
}
