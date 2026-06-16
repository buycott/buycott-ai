package config

import "testing"

func TestExpandEnvVars(t *testing.T) {
	t.Setenv("BUYCOTT_TEST_SET", "secret")

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"set", "key: ${BUYCOTT_TEST_SET}", "key: secret"},
		{"unset expands empty", "key: ${BUYCOTT_TEST_UNSET}", "key: "},
		{"no placeholder", "key: literal", "key: literal"},
		{"multiple", "${BUYCOTT_TEST_SET}-${BUYCOTT_TEST_UNSET}", "secret-"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := expandEnvVars(c.in); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
