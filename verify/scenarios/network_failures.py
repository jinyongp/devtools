"""Installed listener partial-start rollback, retry, and live sibling isolation."""

from pathlib import Path
import socket
import ssl
import sys
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from network_fixture import NetworkFixture, WebSocket, free_port


fixture = NetworkFixture()
with fixture.network():
    certificate = fixture.home / "public-ca.pem"
    fixture.api("proxy", "ca", "export", "--output", str(certificate))
    context = ssl.create_default_context(cafile=str(certificate))
    sibling = {"origin_port": fixture.tls_port, "url": f"https://app.localhost:{fixture.tls_port}"}
    live = WebSocket(sibling, tls_context=context)
    try:
        http_port, tls_port = free_port(), free_port()
        while tls_port == http_port:
            tls_port = free_port()
        fixture.mutate("proxy", "configure", "--listener", "preview",
                       "--http-address", f"127.0.0.1:{http_port}",
                       "--https-address", f"127.0.0.1:{tls_port}")
        operation = str(uuid.uuid4())
        with socket.socket() as busy:
            busy.bind(("127.0.0.1", tls_port))
            busy.listen()
            fixture.api("proxy", "start", "--listener", "preview", "--request-id", operation,
                        error="port_in_use")
            # Opening HTTP succeeded before TLS failed; that first socket must roll back.
            with socket.socket() as probe:
                probe.bind(("127.0.0.1", http_port))
            live.echo()
            fresh = WebSocket(sibling, tls_context=context)
            fresh.close()
            assert not fixture.api("proxy", "status", "--listener", "preview")["item"]["running"]
        started = fixture.api("proxy", "start", "--listener", "preview", "--request-id", operation)
        assert started["item"]["running"]
        assert fixture.api("proxy", "start", "--listener", "preview", "--request-id", operation)["replayed"]
        fixture.mutate("proxy", "stop", "--listener", "preview")
        live.echo()
        fixture.mutate("proxy", "start", "--listener", "preview")
        live.echo()
        assert fixture.api("proxy", "status")["item"]["running"]
        assert fixture.api("proxy", "ca", "status")["item"]["present"]
        print("Installed listener: second-socket failure rolls back HTTP, same request retries, sibling TLS/WebSocket survives failure/stop/restart")
    finally:
        live.close()
