#!/bin/sh
set -eu

package_directory="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
export WARMMO_SKILLS_DIR="${package_directory}/skills"

exec "${package_directory}/bin/warmmo-core"
