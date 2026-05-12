package domain

// Version is the build's identifying string. The default value "dev" is
// overwritten at link time by the Makefile via:
//
//	-ldflags "-X github.com/mandarnilange/corvee/internal/domain.Version=<value>"
//
// Release builds inject `git describe --tags --always --dirty`.
var Version = "dev"
