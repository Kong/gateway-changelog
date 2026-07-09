#!/usr/bin/env python3
"""Re-check changelog files against changelog-schema.json and print one
GitHub error annotation per violation, with a human-readable reason.

Runs only after yaml-schema-checker has already failed the job, so this
script is best-effort: it exits 0 regardless, and the schema stays the
single source of truth (enums and the Plugin message pattern are read
from it).

Usage: explain-changelog-errors.py <schema.json> <comma-separated globs>
"""

import glob
import json
import re
import sys


def error(file, msg):
    print(f"::error file={file}::{file}: {msg}")


def main():
    schema_file, patterns = sys.argv[1], sys.argv[2]

    with open(schema_file) as f:
        schema = json.load(f)

    props = schema["properties"]
    type_enum = props["type"]["enum"]
    scope_enum = props["scope"]["enum"]
    jira_pattern = props["jiras"]["items"]["pattern"]
    msg_min = props["message"]["minLength"]
    msg_max = props["message"]["maxLength"]
    plugin_pattern = schema["then"]["properties"]["message"]["pattern"]

    try:
        import yaml
    except ImportError:
        print("::warning::PyYAML unavailable; cannot print detailed changelog hints")
        return

    files = []
    for pattern in patterns.split(","):
        files.extend(sorted(glob.glob(pattern.strip(), recursive=True)))
    if not files:
        print(f"::warning::no changelog files matched: {patterns}")
        return

    for file in files:
        try:
            with open(file) as f:
                doc = yaml.safe_load(f)
        except yaml.YAMLError as e:
            error(file, f"invalid YAML: {e}")
            continue

        if not isinstance(doc, dict):
            error(file, "changelog must be a YAML mapping with 'message' and 'type' keys")
            continue

        # keys are case-sensitive: "Scope: Plugin" silently becomes an
        # unknown key and the entry loses its scope
        for key in doc:
            if key in props:
                continue
            lowered = str(key).lower()
            if lowered in props:
                error(file, f"unknown key \"{key}\" — keys are case-sensitive; did you mean '{lowered}'?")
            else:
                error(file, f"unknown key \"{key}\" — allowed keys: {', '.join(props)}")

        message = doc.get("message")
        if message is None:
            error(file, "'message' is required")
        elif not isinstance(message, str):
            error(file, "'message' must be a string")
        elif not (msg_min <= len(message) <= msg_max):
            error(file, f"'message' must be {msg_min}-{msg_max} characters, got {len(message)}")

        def enum_error(field, value, enum):
            match = next((e for e in enum if e.lower() == str(value).lower()), None)
            if match is not None:
                error(file, f"'{field}' value \"{value}\" has wrong casing — values are case-sensitive; use \"{match}\"")
            else:
                error(file, f"'{field}' must be one of {', '.join(enum)}; got \"{value}\"")

        type_ = doc.get("type")
        if type_ is None:
            error(file, "'type' is required")
        elif type_ not in type_enum:
            enum_error("type", type_, type_enum)

        scope = doc.get("scope")
        if scope is not None and scope not in scope_enum:
            enum_error("scope", scope, scope_enum)

        if scope == "Plugin" and isinstance(message, str) and not re.match(plugin_pattern, message):
            head = message.splitlines()[0][:60] if message else ""
            hint = ""
            if message.startswith(('"', "'")):
                hint = (" Note: the message starts with a quote character — quotes inside a "
                        "YAML block scalar (| or >) are part of the content; remove them.")
            error(file, "scope is \"Plugin\", so 'message' must start with one or more "
                        "comma-separated plugin names in bold, followed by a space, e.g. "
                        "\"**rate-limiting** Fixed an issue ...\" or "
                        "\"**kafka-upstream**, **confluent**: Added ...\". "
                        f"Actual message starts with: {head!r}.{hint}")

        for key in ("prs", "githubs"):
            val = doc.get(key)
            if val is not None:
                if not isinstance(val, list) or any(not isinstance(i, int) or isinstance(i, bool) for i in val):
                    error(file, f"'{key}' must be a list of integers, e.g. [1001, 1002]")

        jiras = doc.get("jiras")
        if jiras is not None:
            if not isinstance(jiras, list):
                error(file, "'jiras' must be a list of Jira ticket IDs, e.g. [\"FTI-1234\"]")
            else:
                for j in jiras:
                    if not isinstance(j, str) or not re.match(jira_pattern, j):
                        error(file, f"'jiras' entry \"{j}\" must look like a Jira ticket ID, e.g. \"FTI-1234\"")


if __name__ == "__main__":
    main()
