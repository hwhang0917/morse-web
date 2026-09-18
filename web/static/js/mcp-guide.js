const mcpUrl = `${location.origin}/mcp`;

for (const element of document.querySelectorAll('#url, #cc, #cursor, #vscode')) {
  element.textContent = element.textContent.replace('/mcp', mcpUrl);
}
