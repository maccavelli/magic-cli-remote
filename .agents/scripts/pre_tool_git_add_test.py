import sys
import json
import subprocess
import shlex
import os

def main():
    try:
        input_data = json.load(sys.stdin)
    except Exception as e:
        sys.stderr.write(f"Failed to parse hook stdin JSON: {e}\n")
        print(json.dumps({"decision": "allow"}))
        sys.exit(0)

    tool_call = input_data.get("toolCall", {})
    tool_name = tool_call.get("name", "")
    args = tool_call.get("args", {})
    command_line = args.get("CommandLine", "")

    # Check if this is a git add command
    is_git_add = False
    if tool_name == "run_command" and command_line:
        try:
            tokens = shlex.split(command_line)
            if len(tokens) >= 2 and tokens[0] == "git" and tokens[1] == "add":
                is_git_add = True
        except Exception as e:
            # If parsing fails, fall back to simple string check
            if "git add" in command_line:
                is_git_add = True

    if is_git_add:
        sys.stderr.write(f"Intercepted git add command: '{command_line}'. Running unit tests...\n")
        
        # Decide whether to run 'make test' or 'go test ./...'
        has_makefile = os.path.exists("Makefile")
        
        try:
            if has_makefile:
                sys.stderr.write("Executing 'make test-all'...\n")
                res = subprocess.run(["make", "test-all"], capture_output=True, text=True)
            else:
                sys.stderr.write("Executing 'go test ./...'...\n")
                go_res = subprocess.run(["go", "test", "./..."], capture_output=True, text=True)
                if go_res.returncode != 0:
                    res = go_res
                else:
                    sys.stderr.write("Executing 'flutter test' in apps/mobile...\n")
                    res = subprocess.run(["flutter", "test"], cwd="apps/mobile", capture_output=True, text=True)
                
            if res.returncode == 0:
                sys.stderr.write("Unit tests passed successfully!\n")
                print(json.dumps({
                    "decision": "allow", 
                    "reason": "All unit tests passed."
                }))
            else:
                sys.stderr.write("Unit tests FAILED!\n")
                sys.stderr.write(res.stdout + "\n" + res.stderr + "\n")
                print(json.dumps({
                    "decision": "deny",
                    "reason": f"Unit tests failed with exit code {res.returncode}. Output:\n{res.stdout}\n{res.stderr}"
                }))
        except Exception as e:
            sys.stderr.write(f"Failed to execute tests: {e}\n")
            print(json.dumps({
                "decision": "deny",
                "reason": f"Failed to run unit tests: {e}"
            }))
    else:
        # Not a git add command, allow execution
        print(json.dumps({"decision": "allow"}))

if __name__ == "__main__":
    main()
