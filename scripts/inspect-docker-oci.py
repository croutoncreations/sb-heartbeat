#!/usr/bin/env python3
"""Validate the local SB Heartbeat multi-architecture OCI archive."""

import io
import json
import struct
import sys
import tarfile


EXPECTED = {
    "linux/amd64": 62,
    "linux/arm64": 183,
}
PT_INTERP = 3


def fail(message: str) -> None:
    raise SystemExit(f"Docker OCI inspection: {message}")


def main() -> None:
    if len(sys.argv) != 3:
        fail("usage: inspect-docker-oci.py ARCHIVE VERSION")
    archive_path, expected_version = sys.argv[1:]

    with tarfile.open(archive_path, "r:*") as archive:
        def read_json(path: str) -> dict:
            member = archive.extractfile(path)
            if member is None:
                fail(f"missing {path}")
            return json.load(member)

        def read_blob(digest: str) -> bytes:
            algorithm, value = digest.split(":", 1)
            if algorithm != "sha256":
                fail(f"unsupported digest {digest}")
            member = archive.extractfile(f"blobs/sha256/{value}")
            if member is None:
                fail(f"missing blob {digest}")
            return member.read()

        manifests: dict[str, dict] = {}
        runnable_platforms: set[str] = set()

        def collect(index: dict) -> None:
            for descriptor in index.get("manifests", []):
                media_type = descriptor.get("mediaType", "")
                document = json.loads(read_blob(descriptor["digest"]))
                if media_type.endswith("image.index.v1+json"):
                    collect(document)
                    continue
                annotations = descriptor.get("annotations", {})
                if annotations.get("vnd.docker.reference.type") == "attestation-manifest":
                    continue
                platform = descriptor.get("platform", {})
                key = f"{platform.get('os', '')}/{platform.get('architecture', '')}"
                if key == "/":
                    fail("runnable image manifest has no platform")
                if key in runnable_platforms:
                    fail(f"duplicate manifest for {key}")
                runnable_platforms.add(key)
                manifests[key] = document

        collect(read_json("index.json"))
        if runnable_platforms != set(EXPECTED):
            fail(f"platforms are {sorted(runnable_platforms)}, want {sorted(EXPECTED)}")

        for platform, expected_machine in EXPECTED.items():
            manifest = manifests[platform]
            config = json.loads(read_blob(manifest["config"]["digest"]))
            os_name, architecture = platform.split("/", 1)
            if config.get("os") != os_name or config.get("architecture") != architecture:
                fail(f"config platform mismatch for {platform}")
            runtime = config.get("config", {})
            if runtime.get("User") != "65532:65532":
                fail(f"runtime user mismatch for {platform}")
            if runtime.get("Entrypoint") != ["/usr/local/bin/sb-heartbeat"]:
                fail(f"entrypoint mismatch for {platform}")
            labels = runtime.get("Labels", {})
            if labels.get("org.opencontainers.image.version") != expected_version:
                fail(f"version label mismatch for {platform}")

            binary = None
            for layer in manifest.get("layers", []):
                with tarfile.open(fileobj=io.BytesIO(read_blob(layer["digest"])), mode="r:*") as layer_tar:
                    for name in ("usr/local/bin/sb-heartbeat", "./usr/local/bin/sb-heartbeat"):
                        try:
                            member = layer_tar.extractfile(name)
                        except KeyError:
                            member = None
                        if member is not None:
                            binary = member.read()
                            break
                if binary is not None:
                    break
            inspect_elf(binary, platform, expected_machine)

    print("Docker OCI inspection: PASS")


def inspect_elf(binary: bytes | None, platform: str, expected_machine: int) -> None:
    if binary is None or len(binary) < 64 or binary[:4] != b"\x7fELF":
        fail(f"missing ELF binary for {platform}")
    if binary[4] != 2 or binary[5] != 1:
        fail(f"binary for {platform} is not little-endian ELF64")
    e_machine = struct.unpack_from("<H", binary, 18)[0]
    if e_machine != expected_machine:
        fail(f"e_machine {e_machine} is wrong for {platform}")
    program_offset = struct.unpack_from("<Q", binary, 32)[0]
    entry_size = struct.unpack_from("<H", binary, 54)[0]
    entry_count = struct.unpack_from("<H", binary, 56)[0]
    for index in range(entry_count):
        offset = program_offset + index * entry_size
        if offset + 4 > len(binary):
            fail(f"invalid program headers for {platform}")
        if struct.unpack_from("<I", binary, offset)[0] == PT_INTERP:
            fail(f"binary for {platform} has a dynamic interpreter")


if __name__ == "__main__":
    main()
