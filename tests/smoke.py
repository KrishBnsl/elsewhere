"""Exercise the player in a pseudo-terminal with local WAV and FLAC tracks."""

import fcntl
import math
import os
from pathlib import Path
import pty
import re
import select
import struct
import subprocess
import tempfile
import termios
import time
import wave
import zlib


def wait_for(terminal, expected):
    deadline = time.monotonic() + 8
    output = bytearray()
    while expected not in output and time.monotonic() < deadline:
        if select.select([terminal], [], [], 0.1)[0]:
            try:
                output.extend(os.read(terminal, 65536))
            except OSError:
                break
    assert expected in output, output[-3000:]
    return output


def png_chunk(kind, data):
    return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))


with tempfile.TemporaryDirectory() as temporary:
    root = Path(temporary)
    executable = root / "elsewhere"
    source = Path(__file__).resolve().parents[1]
    subprocess.run(["go", "build", "-o", str(executable), "./cmd/elsewhere"], cwd=source, check=True)

    album = root / "Test Album"
    album.mkdir()
    (album / "cover.png").write_bytes(
        b"\x89PNG\r\n\x1a\n"
        + png_chunk(b"IHDR", struct.pack(">IIBBBBB", 1, 1, 8, 2, 0, 0, 0))
        + png_chunk(b"IDAT", zlib.compress(b"\0\xff\0\0"))
        + png_chunk(b"IEND", b"")
    )
    for title in ("01 First Light", "02 Blue Hour"):
        with wave.open(str(album / (title + ".wav")), "wb") as audio:
            audio.setparams((1, 2, 44100, 0, "NONE", "not compressed"))
            tone = b"".join(struct.pack("<h", int(12_000 * math.sin(2 * math.pi * 440 * i / 44100)))
                            for i in range(44100))
            audio.writeframes(tone * 15)

    wav = album / "02 Blue Hour.wav"
    subprocess.run(["afconvert", "-f", "flac", "-d", "flac", str(wav), str(wav.with_suffix(".flac"))], check=True)
    wav.unlink()
    wav.with_suffix(".lrc").write_text("[00:00.00]First lyric\n[00:10.00]Second lyric\n", encoding="utf-8")

    terminal, child = pty.openpty()
    fcntl.ioctl(child, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 95, 0, 0))
    original = termios.tcgetattr(child)
    env = {**os.environ, "HOME": str(root), "TERM": "xterm-256color", "TERM_PROGRAM": "", "TMUX": "", "STY": ""}
    with subprocess.Popen([str(executable), str(root)], stdin=child, stdout=child, stderr=child, env=env) as player:
        try:
            startup = wait_for(terminal, b"Test Album")
            assert startup.index(b"Liked Songs (0)") < startup.index(b"Test Album")
            os.write(terminal, b"/radio\r")
            picker = wait_for(terminal, b"Enter Tune in")
            for station in (b"Chiptune Radio", b"Anime FM", b"Game OST", b"Remix Lab"):
                assert station in picker
            os.write(terminal, b"rmx lab")
            wait_for(terminal, "› Remix Lab".encode())
            os.write(terminal, b"\x1b/missing\r")
            wait_for(terminal, b"0 albums")
            os.write(terminal, b"/\x1b")
            wait_for(terminal, b"Test Album")
            os.write(terminal, b"/np\r")
            wait_for(terminal, b"NEW PLAYLIST")
            os.write(terminal, b"Late Night\tSongs after dark\r")
            wait_for(terminal, b"Late Night (0)")
            os.write(terminal, b"/np\r")
            wait_for(terminal, b"NEW PLAYLIST")
            os.write(terminal, b"Morning Drive\r")
            wait_for(terminal, b"Morning Drive (0)")
            os.write(terminal, b"\t\x1b[B\r")
            popup = wait_for(terminal, b"Esc Library")
            assert b"02 Blue Hour  /  Unknown artist" in popup
            assert b"\x1b[38;2;255;0;0m\x1b[48;2;255;0;0m" + "▀".encode() * 32 in popup
            assert ("┌─ NOW PLAYING " + "─" * 70).encode() in popup
            assert b"APP 70%" in popup
            assert b"SPECTRUM / AMBER RAIN" in popup
            assert "█".encode() in popup or "▇".encode() in popup
            os.write(terminal, b"+")
            wait_for(terminal, b"APP 75%")
            os.write(terminal, b"-")
            wait_for(terminal, b"APP 70%")
            wait_for(terminal, b"PLAY  0:01")
            os.write(terminal, b"L")
            lyrics = wait_for(terminal, "\x1b[1m› First lyric".encode())
            assert b"SPECTRUM / AMBER RAIN" not in lyrics

            def row_of(output, label):
                before_label = output.split(label, 1)[0]
                return int(re.findall(rb"\x1b\[(\d+);1H", before_label)[-1])

            heading_row = row_of(lyrics, b"SYNCED LYRICS")
            assert heading_row == row_of(popup, b"TRACK 2/2") + 2
            assert row_of(lyrics, "› First lyric".encode()) == heading_row + 3
            os.write(terminal, b"\x1b[C")
            wait_for(terminal, "\x1b[1m› Second lyric".encode())
            os.write(terminal, b"\x1b[D")
            wait_for(terminal, "\x1b[1m› First lyric".encode())
            os.write(terminal, b"L")
            wait_for(terminal, b"SPECTRUM / AMBER RAIN")
            os.write(terminal, b"\x1b")
            wait_for(terminal, "› TRACKS".encode())
            os.write(terminal, b"/ap\r")
            picker = wait_for(terminal, b"Enter Add")
            assert b"02 Blue Hour / Unknown artist" in picker
            os.write(terminal, b"mrngdrv\r")
            wait_for(terminal, b"Saved to Morning Drive")
            os.write(terminal, b"m")
            wait_for(terminal, b"Esc Library")
            os.write(terminal, b"<")
            wait_for(terminal, b"01 First Light  /  Unknown artist")
            os.write(terminal, b">")
            wait_for(terminal, b"02 Blue Hour  /  Unknown artist")
            os.write(terminal, b"<")
            wait_for(terminal, b"01 First Light  /  Unknown artist")
            os.write(terminal, b"\x1b")
            wait_for(terminal, "› TRACKS".encode())
            os.write(terminal, b"\x1b[A\r")
            wait_for(terminal, b"Esc Library")
            os.write(terminal, b" ")
            wait_for(terminal, b"PAUSE")
            os.write(terminal, b"v")
            without_visuals = wait_for(terminal, b"V Visuals off")
            assert b"SPECTRUM / AMBER RAIN" not in without_visuals
            os.write(terminal, b"v")
            with_visuals = wait_for(terminal, b"V Visuals on")
            assert b"SPECTRUM / AMBER RAIN" in with_visuals
            os.write(terminal, b"*")
            wait_for(terminal, b"* Unlike")
            os.write(terminal, b"/ap\r")
            picker = wait_for(terminal, b"Enter Add")
            assert b"ADD TO PLAYLIST" in picker
            assert b"Late Night / Songs after dark" in picker
            assert b"Morning Drive" in picker
            os.write(terminal, b"ltnght\r")
            wait_for(terminal, b"Saved to Late Night")
            os.write(terminal, b":q\r")
            wait_for(terminal, b"\x1b[?1049l")
            assert player.wait(timeout=5) == 0
            assert termios.tcgetattr(child) == original
        finally:
            if player.poll() is None:
                player.kill()
            os.close(terminal)
            os.close(child)
    terminal, child = pty.openpty()
    fcntl.ioctl(child, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 95, 0, 0))
    with subprocess.Popen([str(executable), str(root)], stdin=child, stdout=child, stderr=child, env=env) as player:
        try:
            saved = wait_for(terminal, b"Morning Drive (1)")
            assert saved.index(b"Liked Songs (1)") < saved.index(b"Late Night (1)")
            os.write(terminal, b"\r")
            popup = wait_for(terminal, b"* Unlike")
            assert b"TRACK 1/1" in popup
            assert b"\x1b[38;2;255;0;0m" in popup
            os.write(terminal, b"*")
            unliked = wait_for(terminal, b"* Like")
            assert b"Liked Songs (0)" in unliked
            os.write(terminal, b"\x1b")
            wait_for(terminal, b"Press * on a track")
            os.write(terminal, b"\x1b[B\r")
            playlist_popup = wait_for(terminal, b"Esc Library")
            assert b"TRACK 1/1" in playlist_popup
            assert b"01 First Light" in playlist_popup
            assert b"\x1b[38;2;255;0;0m" in playlist_popup
            os.write(terminal, b":q\r")
            wait_for(terminal, b"\x1b[?1049l")
            assert player.wait(timeout=5) == 0
        finally:
            if player.poll() is None:
                player.kill()
            os.close(terminal)
            os.close(child)
    terminal, child = pty.openpty()
    fcntl.ioctl(child, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 95, 0, 0))
    with subprocess.Popen([str(executable), str(root)], stdin=child, stdout=child, stderr=child,
                          env={**env, "TERM": "xterm-kitty"}) as player:
        try:
            wait_for(terminal, b"Test Album")
            os.write(terminal, b"\r")
            image = wait_for(terminal, b";iVBORw0KGgo")
            assert b"\x1b_Ga=T,f=100,t=d" in image
            assert "▀".encode() not in image
            os.write(terminal, b"\x1b")
            wait_for(terminal, b"\x1b_Ga=d,d=I")
            os.write(terminal, b":q\r")
            wait_for(terminal, b"\x1b[?1049l")
            assert player.wait(timeout=5) == 0
        finally:
            if player.poll() is None:
                player.kill()
            os.close(terminal)
            os.close(child)
print("Terminal playback smoke test passed.")
