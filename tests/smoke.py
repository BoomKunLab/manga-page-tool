"""Exercise the built CLI with synthetic grayscale PNG pages."""

import pathlib
import struct
import subprocess
import sys
import tempfile
import zlib

PNG_SIGNATURE = b"\x89PNG\r\n\x1a\n"


def chunk(kind, data):
    return (
        struct.pack(">I", len(data))
        + kind
        + data
        + struct.pack(">I", zlib.crc32(kind + data))
    )


def make_page(content):
    width, height = 12, 10
    left, top, right, bottom = content
    rows = bytearray()
    for y in range(height):
        rows.append(0)  # PNG filter: None.
        for x in range(width):
            inside = left <= x < right and top <= y < bottom
            rows.append(0 if inside or y == height - 1 else 255)
    header = struct.pack(">IIBBBBB", width, height, 8, 0, 0, 0, 0)
    return (
        PNG_SIGNATURE
        + chunk(b"IHDR", header)
        + chunk(b"IDAT", zlib.compress(rows))
        + chunk(b"IEND", b"")
    )


def png_size(path):
    with path.open("rb") as image:
        header = image.read(24)
    assert header.startswith(PNG_SIGNATURE)
    assert header[12:16] == b"IHDR"
    return struct.unpack(">II", header[16:24])


def main():
    binary = pathlib.Path(sys.argv[1]).resolve()
    with tempfile.TemporaryDirectory(prefix="mangatool-smoke-") as directory:
        root = pathlib.Path(directory)
        pages = root / "pages with space"
        pages.mkdir()
        sources = {
            "2.png": make_page((2, 3, 7, 7)),
            "10.png": make_page((4, 2, 10, 8)),
            "11.png": make_page((0, 0, 0, 0)),
        }
        for name, content in sources.items():
            (pages / name).write_bytes(content)

        subprocess.run([binary, "crop", pages], check=True)
        cropped = root / (pages.name + "_cropped")
        assert {path.name for path in cropped.iterdir()} == set(sources)
        for name in sources:
            assert png_size(cropped / name) == (8, 6), name

        subprocess.run([binary, "convert", "webp", cropped], check=True)
        webp = root / (cropped.name + "_webp")
        assert {path.name for path in webp.iterdir()} == {
            pathlib.Path(name).stem + ".webp" for name in sources
        }
        for path in webp.iterdir():
            with path.open("rb") as image:
                header = image.read(12)
            assert header[:4] == b"RIFF" and header[8:] == b"WEBP", path

        subprocess.run([binary, "convert", "png", webp], check=True)
        round_trip = root / (webp.name + "_png")
        for name, content in sources.items():
            assert png_size(round_trip / name) == (8, 6), name
            assert (pages / name).read_bytes() == content, name


if __name__ == "__main__":
    main()
