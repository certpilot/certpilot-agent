#!/bin/sh
# Stop what is running before the binary goes. The identity in
# /var/lib/certpilot-agent is left: removing a package must not un-enrol a host.
if [ -d /run/systemd/system ]; then
  systemctl disable --now certpilot-agent.service certpilot-agent.timer >/dev/null 2>&1 || true
fi
