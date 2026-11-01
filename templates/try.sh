#!/bin/sh
# Writes each template as agk brick init would, builds its image and runs its cases with agk brick
# test: a template that does not build, or whose example does not hold, is one a brick author
# would meet first.
#
#     templates/try.sh [go|python|typescript]...
#
# The Go template requires github.com/agentiik/brick-sdk at the release; until that tag exists, and
# to test this checkout rather than the last release, the module is replaced by this repository,
# copied into the build context.
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for lang in "${@:-go python typescript}"; do
  for l in $lang; do
    name="label-$l"
    dir="$work/$name"
    cp -R "$here/templates/$l" "$dir"
    # agk brick init substitutes the brick's name, and nothing else.
    grep -rl '{{name}}' "$dir" | while read -r f; do sed "s/{{name}}/$name/g" "$f" > "$f.tmp" && mv "$f.tmp" "$f"; done
    if [ "$l" = go ]; then
      mkdir "$dir/brick-sdk" && cp -R "$here/go.mod" "$here/brick" "$dir/brick-sdk/"
      (cd "$dir" && go mod edit -replace github.com/agentiik/brick-sdk=./brick-sdk && go mod tidy)
      sed 's#^COPY go.mod go.sum ./#COPY . ./#' "$dir/Dockerfile" > "$dir/Dockerfile.try"
      docker build -q -f "$dir/Dockerfile.try" -t "$name:try" "$dir" >/dev/null
    else
      docker build -q -t "$name:try" "$dir" >/dev/null
    fi
    echo "::group::$l"
    (cd "$dir" && agk brick test --image "$name:try" --ignore meta.run_id,meta.produced_at,files.uri)
    echo "::endgroup::"
  done
done
