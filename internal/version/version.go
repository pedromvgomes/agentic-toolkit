package version

// Version is the agtk CLI version. The default "dev" is overridden at
// release-build time via -ldflags "-X .../version.Version=vX.Y.Z".
var Version = "dev"
