#!/usr/bin/env bash
#
# Install the built deb and rpm in real distributions and check what they put
# on the host.
#
#   ./scripts/verify-packages.sh dist      after build-release.sh wrote dist/
#
# Not run under systemd: a container has none. What is checked is what a
# package can get wrong without anyone noticing until a host is enrolled: the
# binary, the units and whether systemd accepts them, the directories and their
# modes, that nothing starts before enrolment, and that removal cleans up.
#
set -uo pipefail

cd "$(dirname "$0")/.."
dist="${1:?usage: verify-packages.sh DIST}"
dist="$(cd "$dist" && pwd)"
fail=0

check() {
  local image="$1" prep="$2" install="$3" remove="$4" pkg="$5"
  echo "── $image: $pkg"
  if ! docker run --rm --platform linux/amd64 -v "$dist:/dist:ro" "$image" bash -euc "
    $prep >/dev/null 2>&1
    $install /dist/$pkg >/dev/null
    v=\$(certpilot-agent version); echo \"  version \$v\"
    for u in certpilot-agent.service certpilot-agent-once.service certpilot-agent.timer; do
      test -f /usr/lib/systemd/system/\$u || { echo \"  missing \$u\"; exit 1; }
    done
    systemd-analyze verify /usr/lib/systemd/system/certpilot-agent*.service /usr/lib/systemd/system/certpilot-agent.timer
    echo '  systemd accepts the units'
    test \"\$(stat -c %a /var/lib/certpilot-agent)\" = 700 || { echo '  state dir is not 0700'; exit 1; }
    test -d /etc/certpilot || { echo '  no /etc/certpilot'; exit 1; }
    grep -q '^ConditionPathExists=/var/lib/certpilot-agent/agent.json' /usr/lib/systemd/system/certpilot-agent.service \
      || { echo '  the service would start before enrolment'; exit 1; }
    $remove certpilot-agent >/dev/null
    ! command -v certpilot-agent >/dev/null || { echo '  binary left after removal'; exit 1; }
    echo '  installs, and removes cleanly'
  "; then
    echo "  FAILED"; fail=1
  fi
}

deb=$(cd "$dist" && ls certpilot-agent_*_amd64.deb 2>/dev/null | head -1)
rpm=$(cd "$dist" && ls certpilot-agent-*.x86_64.rpm 2>/dev/null | head -1)
[ -n "$deb" ] || { echo "no amd64 .deb in $dist"; exit 1; }
[ -n "$rpm" ] || { echo "no x86_64 .rpm in $dist"; exit 1; }

# systemd is installed only so systemd-analyze can check the units.
check debian:12 "apt-get update -qq && apt-get install -y -qq systemd" "apt-get install -y -qq" "apt-get remove -y -qq" "$deb"
check rockylinux:9 "dnf install -y -q systemd" "dnf install -y -q" "dnf remove -y -q" "$rpm"

exit $fail
