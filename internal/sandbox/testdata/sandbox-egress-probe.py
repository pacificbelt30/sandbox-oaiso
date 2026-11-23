"""Review and apply a shared egress change through the public terminal UI."""
import fcntl
import json
import os
import pty
import select
import signal
import struct
import subprocess
import sys
import termios
import time

binary, project, config, worker, peer, generation = sys.argv[1:]
generation = int(generation)
base = [binary, "--config", config]
env = os.environ | {"TERM": "xterm-256color"}
master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 32, 115, 0, 0))
screen = Screen(rows=32, columns=115)
process = subprocess.Popen(
    base + ["ui"], cwd=project, stdin=slave, stdout=slave, stderr=slave,
    env=env, start_new_session=True, preexec_fn=lambda: fcntl.ioctl(0, termios.TIOCSCTTY, 0),
)
sent_at = -1


def send(keys):
    global sent_at
    sent_at = screen.received
    os.write(master, keys)


def expect(marker, timeout=40):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if marker in screen.text() and screen.received > sent_at:
            return screen.text()
        if process.poll() is not None:
            break
        if select.select([master], [], [], .1)[0]:
            screen.feed(os.read(master, 65536))
    raise AssertionError((marker, screen.text()[-4500:]))


def resize(rows, columns):
    global screen, sent_at
    screen = Screen(rows=rows, columns=columns)
    sent_at = -1
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0))
    os.kill(process.pid, signal.SIGWINCH)


def policy(target, action="show", *args):
    raw = subprocess.check_output(
        base + ["policy", action, "--worker", target, "--json", *args],
        cwd=project, env=env, timeout=40,
    )
    return json.loads(raw)


try:
    expect(b"sort=name")
    send(b"/name:" + worker.encode() + b"\r")
    expect(worker.encode())
    send(b"\x1bOQ")  # F2
    expect(b"Egress denials and permissions")
    send(b"e")
    expect(b"Enter reviews a grant")
    assert b"packages.example.invalid:80" in screen.text()
    send(b"\r")
    expect(b"Review egress policy change")
    assert b"=packages.example.invalid" in expect(b"Confirm")
    assert worker.encode() in screen.text() and peer.encode() in screen.text()
    assert b"all existing HTTP and CONNECT" in screen.text()
    resize(12, 32)
    expect(b"Confirming closes")
    expect(b"Confirm")
    resize(8, 20)
    expect(b"Resize to 24")
    resize(32, 115)
    expect(b"Review egress policy change")
    assert policy(worker)["generation"] == generation
    send(b"\x1b")
    expect(b"Egress denials")
    assert policy(worker)["generation"] == generation
    assert not policy(worker, "explain", "packages.example.invalid:80")["allowed"]
    send(b"\r")
    expect(b"Review egress policy change")
    send(b"\t\t\r")  # Review -> Cancel -> Confirm.
    expect(b"Policy applied")
    assert b"ready" in screen.text()
    assert policy(worker)["generation"] == generation + 1
    assert policy(peer)["generation"] == generation + 1
    assert policy(peer, "explain", "packages.example.invalid:80")["allowed"]
    assert not policy(peer, "explain", "sub.packages.example.invalid:80")["allowed"]
    assert not policy(peer, "explain", "blocked.example:80")["allowed"]
    send(b"\r")
    expect(b"Egress denials")
    send(b"\x1b")
    expect(b"sort=name")
    send(b"q")
    assert process.wait(timeout=10) == 0
    print("PASS: shared denial history, exact-host review, cancel preserves generation, confirmed live change on both Kata workers")
finally:
    if process.poll() is None:
        process.kill()
        process.wait()
    os.close(master)
    os.close(slave)
