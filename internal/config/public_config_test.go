package config

import "testing"

func TestParsePublicListenDefault(t *testing.T) {
	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if cfg.PublicListenAddress !=
		DefaultPublicListenAddress {

		t.Fatalf(
			"PublicListenAddress = %q, want %q",
			cfg.PublicListenAddress,
			DefaultPublicListenAddress,
		)
	}
}

func TestParsePublicListenOverride(t *testing.T) {
	cfg, err := Parse([]string{
		"-listen",
		"127.0.0.1:9110",
		"-public-listen",
		"127.0.0.1:9113",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if cfg.PublicListenAddress != "127.0.0.1:9113" {
		t.Fatalf(
			"PublicListenAddress = %q",
			cfg.PublicListenAddress,
		)
	}
}

func TestParseRejectsNonLoopbackPublicListen(
	t *testing.T,
) {
	for _, address := range []string{
		"0.0.0.0:9103",
		"[::]:9103",
		"192.0.2.10:9103",
		"localhost:9103",
	} {
		t.Run(address, func(t *testing.T) {
			if _, err := Parse([]string{
				"-public-listen",
				address,
			}); err == nil {
				t.Fatalf(
					"accepted non-loopback address %q",
					address,
				)
			}
		})
	}
}

func TestParseRejectsSamePrivateAndPublicAddress(
	t *testing.T,
) {
	if _, err := Parse([]string{
		"-listen",
		"127.0.0.1:9100",
		"-public-listen",
		"127.0.0.1:9100",
	}); err == nil {
		t.Fatal(
			"accepted identical private and public addresses",
		)
	}
}
