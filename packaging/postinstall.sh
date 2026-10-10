#!/bin/sh
# Make systemd see the units. Nothing is enabled or started: the host has to
# enrol first, and which of the service or the timer to use is the operator's
# choice.
if [ -d /run/systemd/system ]; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi
