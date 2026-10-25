.PHONY: setup dev build check format
# Compatibility aliases; Taskfile.yaml owns the commands.
setup dev build check format:
	task $@
