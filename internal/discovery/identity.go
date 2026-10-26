package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Identity is the device identity stored on disk.
type Identity struct {
	Name string `json:"name"`
}

// ConfigDir returns the cthrone config directory.
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "cthrone")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// LoadIdentity loads the device identity from disk, generating a default if missing.
func LoadIdentity() (*Identity, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "device.json")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			hostname, _ := os.Hostname()
			if hostname == "" {
				hostname = "cthrone"
			}
			id := &Identity{Name: hostname}
			if err := SaveIdentity(id); err != nil {
				return nil, err
			}
			return id, nil
		}
		return nil, err
	}
	var id Identity
	if err := json.Unmarshal(b, &id); err != nil {
		return nil, err
	}
	if id.Name == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "cthrone"
		}
		id.Name = hostname
		if err := SaveIdentity(&id); err != nil {
			return nil, err
		}
	}
	return &id, nil
}

// SaveIdentity saves the device identity to disk.
func SaveIdentity(id *Identity) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "device.json")
	b, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}
