// The declared path is the repo, not this directory, so imports read
// …/agentic-toolkit/internal/… rather than repeating source/toolkit in every
// one. The cost is that the Go toolchain cannot fetch this module: it resolves
// a module in a subdirectory only when the declared path is the repo root plus
// that subdirectory. Nothing needs it to — agtk ships as a release archive with
// a self-updater, every package here is internal/, and a declared path ending
// in /source/toolkit would also force release tags to carry that prefix.
module github.com/pedromvgomes/agentic-toolkit

go 1.26.6

require (
	github.com/goccy/go-yaml v1.19.2
	github.com/minio/selfupdate v0.6.0
	github.com/pedromvgomes/agentic-driver v0.7.0
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/spf13/cobra v1.10.2
	golang.org/x/term v0.45.0
)

require (
	aead.dev/minisign v0.2.0 // indirect
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/cloudflare/circl v1.6.5 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)
