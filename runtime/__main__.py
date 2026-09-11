"""Allow ``python -m runtime`` to launch the agent runtime."""
from runtime.main import main

if __name__ == "__main__":
    raise SystemExit(main())
