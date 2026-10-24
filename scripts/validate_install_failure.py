"""Exercise generated Ubuntu failure reporting without networking or disk I/O."""
import contextlib
import io
import runpy
import ssl
import urllib.error
import urllib.request
from unittest.mock import MagicMock, patch


def check_install_failure(path):
    namespace = runpy.run_path(str(path), run_name="test_only")
    response = MagicMock()
    context = object()
    with patch.object(ssl, "create_default_context", return_value=context) as tls, \
            patch.object(urllib.request, "urlopen", return_value=response) as send:
        namespace["report_failure"]()
        tls.assert_called_once()
        request = send.call_args.args[0]
        assert request.full_url.startswith(("https://", "http://127.0.0.1:")) and request.full_url.endswith("/event")
        assert request.data == b'{"state":"failed"}' and request.get_method() == "POST"
        assert request.headers["Authorization"].startswith("Bearer ")
        assert send.call_args.kwargs == {"context": context, "timeout": 15}
        response.__enter__.return_value.read.assert_called_once()
    # Terminal replays are harmless; other errors remain visible, with no
    # credentials or request details included in installer output.
    for code in (410, 401, 500):
        error = urllib.error.HTTPError("https://example/event", code, "private-token-should-not-print", {}, None)
        output = io.StringIO()
        with patch.object(ssl, "create_default_context", return_value=context), \
                patch.object(urllib.request, "urlopen", side_effect=error), \
                contextlib.redirect_stderr(output):
            try:
                runpy.run_path(str(path), run_name="__main__")
            except SystemExit as failure:
                assert failure.code == 1 and code != 410
            else:
                assert code == 410
        assert "private-token" not in output.getvalue()
        assert bool(output.getvalue()) == (code != 410)
