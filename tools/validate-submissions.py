#!/usr/bin/env python3
"""Validate proposal artifacts without trusting Publisher metadata."""
import hashlib
import json
from pathlib import Path
import sys

def decode(data, offset=0):
    marker = data[offset:offset + 1]
    if marker == b"i":
        end = data.index(b"e", offset); return int(data[offset + 1:end]), end + 1
    if marker == b"l":
        values, offset = [], offset + 1
        while data[offset:offset + 1] != b"e":
            value, offset = decode(data, offset); values.append(value)
        return values, offset + 1
    if marker == b"d":
        values, offset = {}, offset + 1
        while data[offset:offset + 1] != b"e":
            key, offset = decode(data, offset); value, offset = decode(data, offset); values[key] = value
        return values, offset + 1
    colon = data.index(b":", offset); length = int(data[offset:colon]); start = colon + 1
    return data[start:start + length], start + length

def verify_torrent(torrent_path, package_path, size):
    payload = torrent_path.read_bytes()
    document, end = decode(payload)
    if end != len(payload) or b"info" not in document: raise ValueError("torrent inválido")
    info = document[b"info"]
    if b"files" in info or info.get(b"name", b"").decode() != package_path.name or info.get(b"length") != size:
        raise ValueError("torrent não corresponde ao pacote")
    actual = b""
    with package_path.open("rb") as package:
        while chunk := package.read(info[b"piece length"]): actual += hashlib.sha1(chunk).digest()
    if actual != info[b"pieces"]: raise ValueError("pieces do torrent não correspondem ao pacote")

def validate(submission_path):
    base = submission_path.parent.resolve()
    document = json.loads(submission_path.read_text())
    torrents = list((base / "torrents").glob("*.torrent"))
    if not torrents: raise ValueError("torrent não encontrado")
    for variant in document["release"]["variants"]:
        spec = variant["package"]
        package_path = (base / spec["path"]).resolve()
        if base not in package_path.parents or not package_path.is_file(): raise ValueError("pacote ausente ou fora da proposta")
        digest = hashlib.sha256()
        size = 0
        with package_path.open("rb") as package:
            while chunk := package.read(1024 * 1024): digest.update(chunk); size += len(chunk)
        if size != spec["sizeBytes"]: raise ValueError("tamanho do pacote divergente")
        if digest.hexdigest() != spec["sha256"]: raise ValueError("SHA-256 do pacote divergente")
        matches = []
        for torrent in torrents:
            try: verify_torrent(torrent, package_path, size); matches.append(torrent)
            except (ValueError, KeyError, UnicodeDecodeError): pass
        if len(matches) != 1: raise ValueError(f"esperado um torrent correspondente para {package_path.name}")
    print(f"proposta válida: {submission_path}")

for argument in sys.argv[1:]:
    try: validate(Path(argument))
    except (OSError, ValueError, KeyError, json.JSONDecodeError) as error:
        print(f"erro: {argument}: {error}", file=sys.stderr); raise SystemExit(1)
