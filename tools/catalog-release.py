#!/usr/bin/env python3
"""Build a signed KMP catalogue head from a schema-2 snapshot."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
from datetime import datetime, timedelta, timezone

sys.path.insert(0, str(Path(__file__).resolve().parent))
from catalog_security import VerificationError, validate_catalog_document


def timestamp(value: datetime) -> str:
    return value.astimezone(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def sign(path: Path, key: Path, output: Path, passphrase_file: Path | None) -> bytes:
    command = ["openssl", "pkeyutl", "-sign", "-rawin", "-inkey", str(key)]
    if passphrase_file is not None:
        command.extend(["-passin", "file:" + str(passphrase_file)])
    command.extend(["-in", str(path), "-out", str(output)])
    subprocess.run(command, check=True)
    return output.read_bytes()


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("catalog", type=Path)
    parser.add_argument("--private-key", required=True, type=Path)
    parser.add_argument("--passphrase-file", type=Path)
    parser.add_argument("--torrent", type=Path,
                        help="metainfo .torrent incorporado ao head assinado")
    parser.add_argument("--magnet", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--valid-days", type=int, default=30)
    parser.add_argument("--next-public-key", type=Path,
                        help="autoriza esta chave para assinar o próximo head")
    args = parser.parse_args()

    raw = args.catalog.read_bytes()
    document = json.loads(raw)
    if not isinstance(document, dict) or document.get("schemaVersion") not in (2, 3):
        parser.error("o catálogo precisa usar schemaVersion 2 ou 3")
    sequence = document.get("sequence")
    if not isinstance(sequence, int) or isinstance(sequence, bool) or sequence < 1:
        parser.error("sequence precisa ser um inteiro positivo")
    if document["schemaVersion"] == 2 and sequence >= 3:
        for index, item in enumerate(document.get("items", []), 1):
            digest = item.get("packageSha256") if isinstance(item, dict) else None
            if not isinstance(digest, str) or len(digest) != 64 or any(
                    char not in "0123456789abcdef" for char in digest):
                parser.error(f"item {index}: packageSha256 obrigatório desde a sequência 3")
    try:
        validate_catalog_document(document, sequence)
    except VerificationError as exc:
        parser.error(str(exc))
    if not args.magnet.startswith("magnet:?xt=urn:btih:"):
        parser.error("magnet inválido")
    if not args.private_key.is_file():
        parser.error("chave privada não encontrada")
    if args.passphrase_file is not None and not args.passphrase_file.is_file():
        parser.error("arquivo de senha não encontrado")
    torrent = None
    if args.torrent is not None:
        if not args.torrent.is_file() or not 1 <= args.torrent.stat().st_size <= 16384:
            parser.error("torrent ausente ou grande demais")
        torrent = args.torrent.read_bytes()

    args.output.mkdir(parents=True, exist_ok=True)
    catalog_name = f"catalog-{sequence:08d}.json"
    catalog_path = args.output / catalog_name
    shutil.copyfile(args.catalog, catalog_path)
    catalog_signature_path = args.output / (catalog_name + ".sig")
    catalog_signature = sign(catalog_path, args.private_key, catalog_signature_path,
                             args.passphrase_file)

    now = datetime.now(timezone.utc).replace(microsecond=0)
    head = {
        "schemaVersion": 1,
        "sequence": sequence,
        "catalogSize": len(raw),
        "catalogSha256": hashlib.sha256(raw).hexdigest(),
        "catalogMagnet": args.magnet,
        "catalogSignature": base64.b64encode(catalog_signature).decode("ascii"),
        "publishedAt": timestamp(now),
        "expiresAt": timestamp(now + timedelta(days=args.valid_days)),
    }
    if torrent is not None:
        head["catalogTorrent"] = base64.b64encode(torrent).decode("ascii")
    if args.next_public_key is not None:
        if not args.next_public_key.is_file() or not 1 <= args.next_public_key.stat().st_size <= 4096:
            parser.error("nova chave pública ausente ou grande demais")
        key_data = args.next_public_key.read_bytes()
        result = subprocess.run(
            ["openssl", "pkey", "-pubin", "-in", str(args.next_public_key),
             "-outform", "DER"], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
            check=False,
        )
        if result.returncode != 0 or not result.stdout:
            parser.error("nova chave pública inválida")
        head["nextKey"] = {
            "effectiveAfterSequence": sequence,
            "publicKey": base64.b64encode(key_data).decode("ascii"),
            "sha256": hashlib.sha256(result.stdout).hexdigest(),
        }
    head_path = args.output / "head.json"
    head_path.write_text(json.dumps(head, ensure_ascii=False, sort_keys=True,
                                    separators=(",", ":")) + "\n", encoding="utf-8")
    sign(head_path, args.private_key, args.output / "head.json.sig",
         args.passphrase_file)
    print(f"release criada em {args.output} (sequência {sequence})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
