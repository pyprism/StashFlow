#!/usr/bin/env bash

version=$1
if [[ -z "$version" ]]; then
  echo "usage: $0 <version>"
  exit 1
fi
package_name=stashflow
binary_name=$package_name

# The full list of the platforms is at: https://golang.org/doc/install/source#environment
platforms=(
#"darwin/amd64"
"darwin/arm64"
"linux/amd64"
"linux/arm"
"linux/arm64"
#"windows/amd64"
)

rm -rf release/
mkdir -p release

for platform in "${platforms[@]}"
do
    platform_split=(${platform//\// })
    os=${platform_split[0]}
    GOOS=${platform_split[0]}
    GOARCH=${platform_split[1]}

    if [ $os = "darwin" ]; then
        os="macOS"
    fi

    output_binary=$binary_name
    if [ $os = "windows" ]; then
        output_binary+='.exe'
    fi

    archive_name=$package_name'-'$version'-'$os'-'$GOARCH

    echo "Building release/$output_binary for $os-$GOARCH..."
    env GOOS=$GOOS GOARCH=$GOARCH go build \
        -ldflags="-s -w -X main.version=$version" \
      -o release/$output_binary ./cmd/stashflow
    if [ $? -ne 0 ]; then
        echo 'An error has occurred! Aborting the script execution...'
        exit 1
    fi

    pushd release > /dev/null
    if [ $os = "windows" ]; then
        zip $archive_name.zip $output_binary
        rm $output_binary
    else
        chmod a+x $output_binary
        gzip -c $output_binary > $archive_name.gz
        rm $output_binary
    fi
    popd > /dev/null
done