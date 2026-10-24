"""Add an inert completion copy to pre-postinst.d Subiquity; retain its UI."""
import os
from subiquity.server.controllers.cmdlist import LateController
from subiquitycore.utils import arun_command

original_run = LateController.run


async def run_with_preload(self, *args, **kwargs):
    # LateController runs only after Install is DONE. Copy before the original
    # run sets run_event, so reboot cannot race the package copy.
    target = self.app.base_model.target
    if not target or os.path.realpath(target) == "/":
        raise RuntimeError("Invalid manual installation target")
    await arun_command(
        ["/bin/sh", "/usr/local/lib/ventoy-preload/copy-to-target.sh", target],
        check=True,
    )
    return await original_run(self, *args, **kwargs)


LateController.run = run_with_preload

if __name__ == "__main__":
    from subiquity.cmd.server import main
    main()
