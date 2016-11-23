#!/bin/bash
set -e

echo -n "Checking platform: "

if [[ "$OSTYPE" == "linux-gnu" ]]; then
  echo "linux"
  URL=$(curl -s https://api.github.com/repos/otm/git-build-state/releases/latest | grep browser_| grep linux_amd64 | awk '{print $2}' | tr -d '"')
elif [[ "$OSTYPE" == "darwin"* ]]; then
  echo "mac"
  URL=$(curl -s https://api.github.com/repos/otm/git-build-state/releases/latest | grep browser_| grep darwin_amd64 | awk '{print $2}' | tr -d '"')
else
  echo $OSTYPE
  echo "Unsuported target platform, sorry"
  exit 1
fi

TARGET=/usr/local/bin/git-build-state

echo "Downloading binary: $URL"
sudo curl -sf -L $URL -o $TARGET
echo "Installed at: $TARGET"
echo "Setting file permissions"
sudo chmod +x $TARGET

exec sudo $TARGET -install </dev/tty >/dev/tty
