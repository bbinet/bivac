package providers

import (
	"io/ioutil"
	"os"
	"testing"

	log "github.com/Sirupsen/logrus"
	"github.com/camptocamp/conplicity/config"
	"github.com/camptocamp/conplicity/handler"
	"github.com/camptocamp/conplicity/orchestrators"
	"github.com/camptocamp/conplicity/volume"
)

func TestGetProvider(t *testing.T) {
	var dir, expected, got string
	var p Provider

	// Test PostgreSQL detection
	expected = "PostgreSQL"
	dir, _ = ioutil.TempDir("", "test_get_provider_postgresql")
	defer os.RemoveAll(dir)
	ioutil.WriteFile(dir+"/PG_VERSION", []byte{}, 0644)

	p = GetProvider(&orchestrators.DockerOrchestrator{}, &volume.Volume{
		Mountpoint: dir,
	})
	got = p.GetName()
	if got != expected {
		t.Fatalf("Expected provider %s, got %s", expected, got)
	}

	// Test MySQL detection
	expected = "MySQL"
	dir, _ = ioutil.TempDir("", "test_get_provider_mysql")
	defer os.RemoveAll(dir)
	os.Mkdir(dir+"/mysql", 0755)

	p = GetProvider(&orchestrators.DockerOrchestrator{}, &volume.Volume{
		Mountpoint: dir,
	})
	got = p.GetName()
	if got != expected {
		t.Fatalf("Expected provider %s, got %s", expected, got)
	}

	// Test OpenLDAP detection
	expected = "OpenLDAP"
	dir, _ = ioutil.TempDir("", "test_get_provider_openldap")
	defer os.RemoveAll(dir)
	ioutil.WriteFile(dir+"/DB_CONFIG", []byte{}, 0644)

	p = GetProvider(&orchestrators.DockerOrchestrator{}, &volume.Volume{
		Mountpoint: dir,
	})
	got = p.GetName()
	if got != expected {
		t.Fatalf("Expected provider %s, got %s", expected, got)
	}

	// Test Default detection
	expected = "Default"
	dir, _ = ioutil.TempDir("", "test_get_provider_default")
	defer os.RemoveAll(dir)

	p = GetProvider(&orchestrators.DockerOrchestrator{}, &volume.Volume{
		Mountpoint: dir,
	})
	got = p.GetName()
	if got != expected {
		t.Fatalf("Expected provider %s, got %s", expected, got)
	}
}

func TestPrepareBackupWithDocker(t *testing.T) {

	// Use Default provider
	dir, _ := ioutil.TempDir("", "test_backup_volume")
	defer os.RemoveAll(dir)

	c := &handler.Conplicity{}
	c.Config = &config.Config{}
	c.Config.Orchestrator = "docker"
	c.Config.Duplicity.Image = "camptocamp/duplicity:latest"
	c.Config.Docker.Endpoint = "unix:///var/run/docker.sock"

	o := orchestrators.GetOrchestrator(c)

	p := &DefaultProvider{
		BaseProvider: &BaseProvider{
			orchestrator: o,
			vol: &volume.Volume{
				Name:       dir,
				Driver:     "local",
				Mountpoint: "/mnt",
			},
		},
	}

	log.SetLevel(log.DebugLevel)

	err := PrepareBackup(p)

	if err != nil {
		t.Fatalf("Expected no error, got error: %v", err)
	}
}

func TestBaseGetHandler(t *testing.T) {
	expected := ""

	p := &BaseProvider{
		orchestrator: &orchestrators.DockerOrchestrator{
			Handler: &handler.Conplicity{},
		},
	}
	got := p.orchestrator.GetHandler().Hostname
	if expected != got {
		t.Fatalf("Expected %s, got %s", expected, got)
	}
}

func TestBaseGetVolume(t *testing.T) {
	got := (&BaseProvider{}).GetVolume()
	if got != nil {
		t.Fatalf("Expected to get nil, got %v", got)
	}
}

func TestBaseGetBackupDir(t *testing.T) {
	got := (&BaseProvider{}).GetBackupDir()
	if got != "" {
		t.Fatalf("Expected to get nil, got %s", got)
	}
}
