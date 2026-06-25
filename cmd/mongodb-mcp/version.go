package main

// version is the binary version. It is overridden at build time via
//
//	-ldflags "-X main.version=<v>"
//
// and the Nix build wires it to the VERSION file.
var version = "dev"
