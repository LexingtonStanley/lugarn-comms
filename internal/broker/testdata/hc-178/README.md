# hc backend end-to-end evidence

Recorded by `python3 scripts/test-hc-178.py --run` against hc 0.1.21 on Linux.
A temporary broker home and private core contain all test state. The pi fixture
is a deterministic Python program that emits OSC 7501 and echoes messages; it
performs no inference and reads no harness configuration. An initial shell-only
stub did not expose foreground metadata, so it could not satisfy adoption's
foreground PID/cwd validation.

The successful scenario covers tabs and workspaces, names including t7, the
runner name folder, real HOME/PATH overrides, status notifications, delivery,
read-only adoption, hidden adopted runner, conversion into the old tab, forget
leaving managed/external members alive, and stop removing managed terminals.
All private cores are stopped and their state removed in finally.

These are CLI/core observations, not evidence of rendering in a Herdr app window.
