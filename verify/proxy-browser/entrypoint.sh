#!/bin/sh
set -eu
if [ -f /artifacts/ca.pem ]; then
    cp /artifacts/ca.pem /usr/local/share/ca-certificates/devtools-fixture.crt
    update-ca-certificates >/dev/null 2>&1
    mkdir -p /root/.pki/nssdb
    certutil -N --empty-password -d sql:/root/.pki/nssdb
    certutil -A -d sql:/root/.pki/nssdb -n devtools-fixture -t 'C,,' -i /artifacts/ca.pem
    export NODE_EXTRA_CA_CERTS=/artifacts/ca.pem
fi
exec node /runner/browser.mjs /artifacts/browser.json
