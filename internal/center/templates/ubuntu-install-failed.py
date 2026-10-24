#!/usr/bin/env python3
# Run only from Subiquity's fatal error hook. No disk or network configuration
# changes: release this job's unfinished state after installation has stopped.
import ssl
import sys
import urllib.error
import urllib.request


def report_failure():
    ca = {{yaml .CA}}
    context = ssl.create_default_context(cadata=ca or None)
    request = urllib.request.Request(
        {{yaml (printf "%s/event" .Base)}},
        data=b'{"state":"failed"}',
        headers={"Authorization": {{yaml (printf "Bearer %s" .Token)}},
                 "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(request, context=context, timeout=15) as response:
            response.read()
    except urllib.error.HTTPError as error:
        # An error hook may run twice; a terminal job must not be reopened.
        if error.code != 410:
            raise


if __name__ == "__main__":
    try:
        report_failure()
    except Exception:
        # Never echo the request, token or an installer exception containing it.
        print("Ventoy Center: could not report installation failure; review the unfinished task before retrying.", file=sys.stderr)
        sys.exit(1)
