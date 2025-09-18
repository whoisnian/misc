#!/bin/bash
#########################################################################
# File Name: update_web.sh
# Author: nian
# Blog: https://whoisnian.com
# Mail: zhuchangbao1998@gmail.com
# Created Time: 2025年09月17日 星期三 09时35分10秒
#########################################################################

SCRIPT_DIR=$(dirname "$0")
OUTPUT_DIR="$SCRIPT_DIR/web"

XTERM_VERSION="5.5.0"
ADDON_ATTACH_VERSION="0.11.0"
ADDON_FIT_VERSION="0.10.0"
ADDON_UNICODE11_VERSION="0.8.0"

# npm view @xterm/xterm dist.tarball
curl -o /tmp/xterm.tgz "https://registry.npmjs.org/@xterm/xterm/-/xterm-${XTERM_VERSION}.tgz"
# npm view @xterm/addon-attach dist.tarball
curl -o /tmp/addon-attach.tgz "https://registry.npmjs.org/@xterm/addon-attach/-/addon-attach-${ADDON_ATTACH_VERSION}.tgz"
# npm view @xterm/addon-fit dist.tarball
curl -o /tmp/addon-fit.tgz "https://registry.npmjs.org/@xterm/addon-fit/-/addon-fit-${ADDON_FIT_VERSION}.tgz"
# npm view @xterm/addon-unicode11 dist.tarball
curl -o /tmp/addon-unicode11.tgz https://registry.npmjs.org/@xterm/addon-unicode11/-/addon-unicode11-${ADDON_UNICODE11_VERSION}.tgz

tar -xzf /tmp/xterm.tgz -C "${OUTPUT_DIR}/static" --strip-components=2 package/lib/xterm.js package/css/xterm.css
tar -xzf /tmp/addon-attach.tgz -C "${OUTPUT_DIR}/static" --strip-components=2 package/lib/addon-attach.js
tar -xzf /tmp/addon-fit.tgz -C "${OUTPUT_DIR}/static" --strip-components=2 package/lib/addon-fit.js
tar -xzf /tmp/addon-unicode11.tgz -C "${OUTPUT_DIR}/static" --strip-components=2 package/lib/addon-unicode11.js

sed -i "s|^//# sourceMappingURL=.*$||g" "${OUTPUT_DIR}/static/"*.js

rm -f /tmp/xterm.tgz
rm -f /tmp/addon-attach.tgz
rm -f /tmp/addon-fit.tgz
rm -f /tmp/addon-unicode11.tgz
