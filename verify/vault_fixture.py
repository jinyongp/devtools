"""Value-free cleanup for sessions created inside an owned Unix test HOME."""
import ctypes
import json
import os
from pathlib import Path
import socket
import struct
import sys


def retire_session(path):
    """Retire the exact test endpoint; never use a stored PID or request keys."""
    endpoint = json.loads(Path(path).read_text())

    def exact(connection, size):
        result = bytearray()
        while len(result) < size:
            chunk = connection.recv(size - len(result))
            if not chunk:
                raise RuntimeError("Private control closed")
            result.extend(chunk)
        return result

    def frame(connection):
        size = struct.unpack('>I', exact(connection, 4))[0]
        if not 0 < size <= 131072:
            raise RuntimeError("Invalid private control frame")
        return json.loads(exact(connection, size))

    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        try:
            connection.connect(endpoint['endpoint'])
        except (FileNotFoundError, ConnectionRefusedError):
            return  # No daemon is reachable; do not start one just for cleanup.
        if sys.platform == 'darwin':
            uid, gid = ctypes.c_uint(), ctypes.c_uint()
            if ctypes.CDLL(None, use_errno=True).getpeereid(connection.fileno(), ctypes.byref(uid), ctypes.byref(gid)) != 0:
                raise RuntimeError("Cannot verify private control peer")
            peer_uid = uid.value
        else:
            _, peer_uid, _ = struct.unpack('3i', connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
        if peer_uid != os.getuid():
            raise RuntimeError("Unexpected private control peer")
        hello = frame(connection)
        if hello.get('control_protocol') != 1 or any(hello.get(key) != endpoint[key] for key in ('protocol', 'vault_id', 'session_id', 'endpoint')):
            raise RuntimeError("Private control identity mismatch")
        request = {key: hello[key] for key in ('protocol', 'vault_id', 'session_id', 'lock_epoch', 'challenge')}
        request['operation'] = 'retire'
        body = json.dumps(request).encode()
        connection.sendall(struct.pack('>I', len(body)) + body)
        response = frame(connection)
        if response.get('error') or response.get('key') is not None:
            raise RuntimeError("Private retirement failed")


def retire_home_sessions(home):
    clean = True
    # The runner owns this whole temporary HOME, including nested transfer roots.
    for path in Path(home).rglob('vault/session.json'):
        try:
            retire_session(path)
        except (OSError, ValueError, KeyError, RuntimeError):
            clean = False
    return clean
