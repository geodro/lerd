package cli

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsGhostContainerError(t *testing.T) {
	// The exact wrapping a unit start produces: the podman stderr is wrapped by
	// the run step. Captured from a user whose lerd-redis was stuck Created with
	// a libpod DB entry but a missing storage layer (DB/storage desync after an
	// unclean Podman Machine shutdown), so `podman run --replace` failed.
	runErr := fmt.Errorf("podman run lerd-redis: %w",
		errors.New(`exit status 125: Error: getting container from store "21ed4c91893d12b7d48773d004b7b0149c9c4b440b08ad3503d22f420ea2ab1d": container not known`))

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"ghost container", runErr, true},
		{"bare from-store wording", errors.New(`getting container from store "abc": container not known`), true},
		{"nil error", nil, false},
		{"overlay corruption is a different heal", errors.New(`getting graph driver info "b77": readlink /var/lib/containers/storage/overlay: invalid argument`), false},
		{"container not known without store context", errors.New("no container with name lerd-redis: container not known"), false},
		{"port conflict", errors.New("rootlessport cannot expose privileged port 80, bind: address already in use"), false},
		{"missing image", errors.New(`short-name "lerd-php85-fpm:local" did not resolve to an alias`), false},
		{"generic failure", errors.New("some other failure"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isGhostContainerError(tc.err); got != tc.want {
				t.Fatalf("isGhostContainerError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
