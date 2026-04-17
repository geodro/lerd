package cli

// recommendedVMMemoryMiB picks Podman Machine memory based on host RAM.
// Returns 4096 (safe default) when detection fails so we never go below
// the floor that fixes the v1.12.6 MySQL+PHP-FPM+Horizon swap thrash.
// int64 matches strconv.ParseInt at the comparison site.
func recommendedVMMemoryMiB(hostMemoryGiB int) int64 {
	switch {
	case hostMemoryGiB <= 0:
		return 4096
	case hostMemoryGiB <= 8:
		return 3072
	case hostMemoryGiB < 32:
		return 4096
	default:
		return 6144
	}
}
