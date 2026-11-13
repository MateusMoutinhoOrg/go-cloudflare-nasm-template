# Puts the tools installed locally by scripts/install.py on the PATH
# (Go and Node.js in engine/tools, Wrangler in engine/node_modules).
# Usage: . engine/env.sh

ENGINE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"

for _bin in "$ENGINE_DIR/node_modules/.bin" "$ENGINE_DIR/tools/node/bin" "$ENGINE_DIR/tools/go/bin"; do
  if [ -d "$_bin" ]; then
    case ":$PATH:" in
      *":$_bin:"*) ;;
      *) PATH="$_bin:$PATH" ;;
    esac
  fi
done
unset _bin
export PATH
