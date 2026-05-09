package config

import "path/filepath"

func pausedServicesFile() string {
	return filepath.Join(DataDir(), "paused-services.yaml")
}

// ServiceIsPaused returns true if the service was manually stopped by the user.
func ServiceIsPaused(name string) bool {
	return serviceSetContains(pausedServicesFile(), name)
}

// SetServicePaused marks or clears the manual-stop flag for the named service.
func SetServicePaused(name string, paused bool) error {
	return serviceSetUpdate(pausedServicesFile(), name, paused)
}
