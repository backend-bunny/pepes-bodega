{
  # Postgres 17 with pgvector, for the process-based test stack (hack/local-stack.sh).
  # The kind cluster uses the pgvector/pgvector image instead.
  description = "postgresql_17 with pgvector";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  outputs = { nixpkgs, ... }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAll = f: nixpkgs.lib.genAttrs systems (s: f nixpkgs.legacyPackages.${s});
    in {
      packages = forAll (pkgs: {
        default = pkgs.postgresql_17.withPackages (p: [ p.pgvector ]);
      });
    };
}
