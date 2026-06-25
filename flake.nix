{
  description = "mongodb-mcp — MCP server for MongoDB over stdio and streamable HTTP";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    { self
    , nixpkgs
    , flake-utils
    }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        version = pkgs.lib.fileContents ./VERSION;
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "mongodb-mcp";
          inherit version;
          src = ./.;

          # vendorHash is kept in sync with go.sum by `just sync-flake` (and CI
          # in .github/workflows/generate.yml). The `# go-sum:` line caches the
          # go.sum digest so sync-flake can skip the nix build when unchanged.
          # go-sum: 05f5e44e60ba250598c026419edd74d1018668c575c82a2f882ff848e2bc8347
          vendorHash = "sha256-kMBk4uC0Yw5XV23m1N8HaoEg7R82x+c8x/OzAiRvYDY=";

          subPackages = [ "cmd/mongodb-mcp" ];

          env.CGO_ENABLED = 0;
          ldflags = [ "-s" "-w" "-X main.version=${version}" ];

          meta = with pkgs.lib; {
            description = "MCP server exposing MongoDB over stdio and streamable HTTP";
            homepage = "https://github.com/stubbedev/mongodb-mcp";
            license = licenses.mit;
            mainProgram = "mongodb-mcp";
          };
        };

        apps.default = flake-utils.lib.mkApp {
          drv = self.packages.${system}.default;
        };

        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.gopls
            pkgs.golangci-lint
            pkgs.gomarkdoc
            pkgs.mongosh
            pkgs.just
          ];
        };

        formatter = pkgs.nixpkgs-fmt;
      });
}
