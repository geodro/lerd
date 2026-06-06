package cli

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsOverlayStorageError(t *testing.T) {
	// The exact wrapping runStart produces: StartUnit wraps the podman stderr.
	realErr := fmt.Errorf("podman run lerd-nginx: %w",
		errors.New(`exit status 125: Error: getting graph driver info "b77616fb55fa": readlink /var/lib/containers/storage/overlay: invalid argument`))

	// Variant 2, captured reproducing a broken container layer in a throwaway
	// Podman Machine: the existing container fails to mount its overlay storage
	// while a fresh `podman run` (new layer, same intact image) still works.
	mountErr := fmt.Errorf("podman run lerd-redis: %w",
		errors.New(`exit status 125: Error: unable to start container "3752898bf744": mounting storage for container 3752898bf744: readlink /var/lib/containers/storage/overlay/312d216688349/diff: no such file or directory`))

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"real overlay corruption", realErr, true},
		{"graph driver readlink+invalid but not overlay store", errors.New(`getting graph driver info "x": readlink /foo: invalid argument`), false},
		{"container mount broken overlay layer", mountErr, true},
		{"nil error", nil, false},
		{"port conflict", errors.New("rootlessport cannot expose privileged port 80, bind: address already in use"), false},
		{"missing image", errors.New(`short-name "lerd-php85-fpm:local" did not resolve to an alias`), false},
		{"unrelated invalid argument", errors.New("some flag: invalid argument"), false},
		{"mounting storage but not overlay", errors.New(`mounting storage for container abc: no such device`), false},
		{"generic failure", errors.New("some other failure"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOverlayStorageError(tc.err); got != tc.want {
				t.Fatalf("isOverlayStorageError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
