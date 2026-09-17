// Fill in this deployment's own MCP URL wherever the guide shows a path.
const url = location.origin + '/mcp';
for (const el of document.querySelectorAll('#url, #cc, #cursor, #vscode')) {
  el.textContent = el.textContent.replace('/mcp', url);
}
