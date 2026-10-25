def storage_sizes():
    # The shell guard overwrites this environment value from verified sysfs
    # capacity before invoking Python. Never fall back to installer defaults.
    raw = os.environ.get("VC_STORAGE_SIZES_MIB", "").split()
    if len(raw) != 4 or any(not re.fullmatch(r"[1-9][0-9]*", n) for n in raw):
        raise ValueError("Missing verified storage sizes")
    sizes = [int(n) for n in raw]
    quarter = sizes[0] // 1280
    if quarter < 1 or quarter & (quarter - 1) or sizes != [n * quarter for n in (1280, 8192, 16384, 32768)]:
        raise ValueError("Invalid capacity tier")
    return list(zip(("tmp", "swap", "var", "root"), sizes)) + [("home", None)]
