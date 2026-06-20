//go:build !linux && !darwin

package dns

// LinkChanges is a no-op on platforms without a native host-network change
// subscription (everything but Linux rtnetlink and macOS PF_ROUTE). The
// safety-net poll in each watcher covers any rare case we still want to
// react to. Returns nil after done closes so callers can use a single
// error-checked code path across platforms.
func LinkChanges(out chan<- struct{}, done <-chan struct{}) error {
	<-done
	return nil
}
