import * as vscode from 'vscode';
import * as cp from 'child_process';
import * as net from 'net';
import * as path from 'path';
import * as fs from 'fs';
import { LumenCompletionProvider } from './completion';

// ── State ───────────────────────────────────────────────────

let serverProcess: cp.ChildProcess | null = null;
let serverPort: number = 0;
let statusBar: vscode.StatusBarItem;
let outputChannel: vscode.OutputChannel;

// ── Activation ──────────────────────────────────────────────

export function activate(context: vscode.ExtensionContext) {
    outputChannel = vscode.window.createOutputChannel('Lumen');
    outputChannel.appendLine('Lumen extension activating...');

    // Status bar
    statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
    statusBar.command = 'lumen.chat';
    statusBar.text = '$(hubot) Lumen';
    statusBar.tooltip = 'Lumen — Click to open chat';
    context.subscriptions.push(statusBar);

    // Register commands
    context.subscriptions.push(
        vscode.commands.registerCommand('lumen.chat', () => openChat(context)),
        vscode.commands.registerCommand('lumen.explain', explainSelection),
        vscode.commands.registerCommand('lumen.fix', fixCurrentFile),
        vscode.commands.registerCommand('lumen.improve', improveSelection),
        vscode.commands.registerCommand('lumen.startServer', startServer),
        vscode.commands.registerCommand('lumen.stopServer', stopServer),
    );

    // Register inline completion provider
    const config = vscode.workspace.getConfiguration('lumen');
    if (config.get<boolean>('completion.enabled', true)) {
        const provider = new LumenCompletionProvider(() => serverPort);
        context.subscriptions.push(
            vscode.languages.registerInlineCompletionItemProvider(
                { pattern: '**' },
                provider
            )
        );
    }

    // Auto-start server
    if (config.get<boolean>('autoStart', true)) {
        startServer();
    }

    // Show status
    statusBar.show();
    outputChannel.appendLine('Lumen extension activated.');
}

export function deactivate() {
    stopServer();
    if (statusBar) { statusBar.dispose(); }
    if (outputChannel) { outputChannel.dispose(); }
}

// ── Server Management ───────────────────────────────────────

async function startServer(): Promise<void> {
    if (serverProcess) {
        vscode.window.showInformationMessage('Lumen server already running on port ' + serverPort);
        return;
    }

    const config = vscode.workspace.getConfiguration('lumen');
    const binaryPath = config.get<string>('binaryPath', 'lumen');
    const requestedPort = config.get<number>('serverPort', 0);

    // Find free port
    serverPort = requestedPort || await findFreePort();
    
    // Find config file
    const configPath = findLumenConfig();
    
    outputChannel.appendLine(`Starting lumen serve on port ${serverPort}...`);
    
    // Spawn the server
    const cwd = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || process.cwd();
    serverProcess = cp.spawn(binaryPath, ['serve', '--addr', `:${serverPort}`], {
        cwd,
        env: { ...process.env },
        stdio: ['ignore', 'pipe', 'pipe'],
    });

    serverProcess.stdout?.on('data', (data: Buffer) => {
        outputChannel.appendLine(`[lumen] ${data.toString().trim()}`);
    });

    serverProcess.stderr?.on('data', (data: Buffer) => {
        outputChannel.appendLine(`[lumen:err] ${data.toString().trim()}`);
    });

    serverProcess.on('close', (code) => {
        outputChannel.appendLine(`Lumen server exited with code ${code}`);
        serverProcess = null;
        serverPort = 0;
        updateStatus('stopped');
    });

    // Wait for server to be ready
    await sleep(1500);
    updateStatus('running');
    vscode.window.showInformationMessage(`Lumen server started on port ${serverPort}`);
}

function stopServer(): void {
    if (serverProcess) {
        serverProcess.kill('SIGTERM');
        serverProcess = null;
        serverPort = 0;
        updateStatus('stopped');
        outputChannel.appendLine('Lumen server stopped.');
    }
}

function updateStatus(state: 'running' | 'stopped' | 'loading') {
    switch (state) {
        case 'running':
            statusBar.text = `$(hubot) Lumen :${serverPort}`;
            statusBar.backgroundColor = undefined;
            break;
        case 'stopped':
            statusBar.text = '$(circle-slash) Lumen';
            statusBar.backgroundColor = new vscode.ThemeColor('statusBarItem.warningBackground');
            break;
        case 'loading':
            statusBar.text = '$(loading~spin) Lumen';
            break;
    }
    statusBar.show();
}

// ── Chat Sidebar ────────────────────────────────────────────

let chatPanel: vscode.WebviewPanel | null = null;

function openChat(context: vscode.ExtensionContext): void {
    if (!serverPort) {
        vscode.window.showWarningMessage('Lumen server not running. Starting...');
        startServer().then(() => {
            if (serverPort) { createChatPanel(context); }
        });
        return;
    }
    createChatPanel(context);
}

function createChatPanel(context: vscode.ExtensionContext): void {
    if (chatPanel) {
        chatPanel.reveal(vscode.ViewColumn.Beside);
        return;
    }

    chatPanel = vscode.window.createWebviewPanel(
        'lumenChat',
        'Lumen Chat',
        vscode.ViewColumn.Beside,
        {
            enableScripts: true,
            retainContextWhenHidden: true,
            localResourceRoots: []
        }
    );

    chatPanel.onDidDispose(() => { chatPanel = null; });

    // Embed the Lumen web UI
    chatPanel.webview.html = getChatHTML(context, serverPort);

    // Handle messages from webview
    chatPanel.webview.onDidReceiveMessage(msg => {
        switch (msg.type) {
            case 'openFile':
                if (msg.path) {
                    const uri = vscode.Uri.file(msg.path);
                    vscode.window.showTextDocument(uri, { viewColumn: vscode.ViewColumn.One });
                }
                break;
            case 'insertCode':
                if (msg.code) {
                    const editor = vscode.window.activeTextEditor;
                    if (editor) {
                        editor.edit(builder => {
                            builder.insert(editor.selection.active, msg.code);
                        });
                    }
                }
                break;
        }
    });
}

function getChatHTML(context: vscode.ExtensionContext, port: number): string {
    return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Lumen Chat</title>
<style>
:root {
  --bg: var(--vscode-editor-background, #1e1e1e);
  --fg: var(--vscode-editor-foreground, #d4d4d4);
  --dim: var(--vscode-descriptionForeground, #888);
  --cyan: var(--vscode-textLink-foreground, #4fc1ff);
  --green: #4ec9b0;
  --yellow: #dcdcaa;
  --red: #f44747;
  --magenta: #c586c0;
  --border: var(--vscode-panel-border, #333);
  --input-bg: var(--vscode-input-background, #2d2d2d);
  --code-bg: var(--vscode-textCodeBlock-background, #252526);
  --font: var(--vscode-font-family, -apple-system, sans-serif);
  --code-font: var(--vscode-editor-font-family, 'Cascadia Code', monospace);
}
* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: var(--font);
  background: var(--bg);
  color: var(--fg);
  display: flex; flex-direction: column; height: 100vh;
  font-size: 13px;
}
#chat { flex: 1; overflow-y: auto; padding: 12px; }
.msg { margin-bottom: 12px; animation: fadeIn 0.15s ease; }
@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }
.msg.user { display: flex; justify-content: flex-end; }
.msg.user .body { background: var(--cyan); color: #000; padding: 6px 12px; border-radius: 12px; max-width: 80%; }
.msg.assistant .body { line-height: 1.6; }
.msg.assistant .body pre {
  background: var(--code-bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 8px 12px;
  overflow-x: auto;
  margin: 6px 0;
  font-family: var(--code-font);
  font-size: 12px;
  position: relative;
}
.msg.assistant .body pre .copy-btn {
  position: absolute; top: 4px; right: 4px;
  background: var(--border); border: none; color: var(--fg);
  padding: 2px 6px; border-radius: 3px; cursor: pointer;
  font-size: 10px; display: none;
}
.msg.assistant .body pre:hover .copy-btn { display: block; }
.msg.assistant .body pre .insert-btn {
  position: absolute; top: 4px; right: 60px;
  background: var(--cyan); color: #000; border: none;
  padding: 2px 6px; border-radius: 3px; cursor: pointer;
  font-size: 10px; display: none;
}
.msg.assistant .body pre:hover .insert-btn { display: block; }
.msg.assistant .body code {
  font-family: var(--code-font); font-size: 12px;
  background: var(--code-bg); padding: 1px 4px; border-radius: 2px;
}
.tool { color: var(--dim); font-size: 12px; margin: 2px 0; }
.tool .ok { color: var(--green); }
.tool .err { color: var(--red); }
.tool .name { color: var(--yellow); }
#input-area {
  padding: 8px 12px;
  border-top: 1px solid var(--border);
  display: flex; gap: 8px;
  background: var(--bg);
}
#input {
  flex: 1;
  background: var(--input-bg);
  border: 1px solid var(--border);
  color: var(--fg);
  padding: 8px 12px;
  border-radius: 6px;
  font-size: 13px;
  font-family: inherit;
  resize: none;
  outline: none;
  min-height: 36px;
  max-height: 120px;
}
#input:focus { border-color: var(--cyan); }
#send {
  background: var(--cyan);
  color: #000;
  border: none;
  padding: 8px 16px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
}
#send:hover { opacity: 0.85; }
#send:disabled { opacity: 0.4; }
#status { padding: 4px 12px; color: var(--dim); font-size: 11px; border-top: 1px solid var(--border); display: flex; gap: 12px; }
#status span { white-space: nowrap; }
</style>
</head>
<body>

<div id="chat"></div>
<div id="input-area">
  <textarea id="input" rows="1" placeholder="Ask Lumen... (Shift+Enter for newline)"></textarea>
  <button id="send">Send</button>
</div>
<div id="status">
  <span id="st-tokens">—</span>
  <span id="st-cost">—</span>
  <span id="st-model">model: —</span>
</div>

<script>
const SERVER = 'http://localhost:${port}';
let running = false;
let tokensIn = 0, tokensOut = 0;

const input = document.getElementById('input');
input.addEventListener('keydown', e => {
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
});

document.getElementById('send').addEventListener('click', send);

async function send() {
  const prompt = input.value.trim();
  if (!prompt || running) return;
  input.value = '';
  input.style.height = 'auto';
  running = true;
  document.getElementById('send').disabled = true;

  appendMsg('user', prompt);
  const el = appendMsg('assistant', '');

  try {
    const resp = await fetch(SERVER + '/v1/chat', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({prompt})
    });
    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buf = '';
    while (true) {
      const {done, value} = await reader.read();
      if (done) break;
      buf += decoder.decode(value, {stream: true});
      const lines = buf.split('\\n');
      buf = lines.pop();
      for (const line of lines) {
        if (!line.startsWith('data: ')) continue;
        try {
          handleEvent(JSON.parse(line.slice(6)), el);
        } catch(e) {}
      }
    }
  } catch(e) {
    el.querySelector('.body').textContent += '\\n⚠ Connection lost — check if lumen serve is running';
  }

  running = false;
  document.getElementById('send').disabled = false;
  input.focus();
}

function handleEvent(ev, el) {
  const body = el.querySelector('.body');
  switch(ev.kind) {
    case 'text':
      body.textContent += ev.text || '';
      // Convert code fences to copy/insert buttons
      break;
    case 'tool_dispatch':
      if (ev.tool) {
        const div = document.createElement('div'); div.className = 'tool';
        div.innerHTML = '🔧 <span class="name">' + ev.tool.name + '</span>';
        el.appendChild(div);
      }
      break;
    case 'tool_result':
      if (ev.tool) {
        const divs = el.querySelectorAll('.tool');
        const div = divs[divs.length-1];
        if (div) {
          div.innerHTML += ev.tool.err
            ? ' <span class="err">✗</span>'
            : ' <span class="ok">✓</span>';
        }
      }
      break;
    case 'usage':
      if (ev.usage) {
        tokensIn += ev.usage.prompt_tokens || 0;
        tokensOut += ev.usage.completion_tokens || 0;
      }
      break;
  }
  updateStatus();
  el.scrollIntoView({behavior: 'smooth'});
}

function appendMsg(role, text) {
  const div = document.createElement('div'); div.className = 'msg ' + role;
  const body = document.createElement('div'); body.className = 'body';
  if (role === 'user') body.textContent = text;
  else body.textContent = '';
  div.appendChild(body);
  document.getElementById('chat').appendChild(div);
  return div;
}

function updateStatus() {
  const tk = tokensIn + tokensOut;
  document.getElementById('st-tokens').textContent = (tk/1000).toFixed(0) + 'k tokens';
}

// Init
fetch(SERVER + '/v1/models').then(r => r.json()).then(d => {
  document.getElementById('st-model').textContent = 'model: ' + (d.provider||'') + '/' + (d.model||'');
}).catch(() => {});

vscode.postMessage({type: 'ready'});
</script>
</body>
</html>`;
}

// ── Editor Commands ─────────────────────────────────────────

async function explainSelection(): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor) { return; }
    const selection = editor.document.getText(editor.selection);
    if (!selection) {
        vscode.window.showInformationMessage('Select code to explain.');
        return;
    }
    await runQuickTask(`Explain this code:\n\`\`\`\n${selection}\n\`\`\``);
}

async function fixCurrentFile(): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor) { return; }
    const code = editor.document.getText();
    const lang = editor.document.languageId;
    const result = await runQuickTask(
        `Fix bugs and issues in this ${lang} file. Return ONLY the fixed code, no explanation:\n\`\`\`${lang}\n${code}\n\`\`\``
    );
    if (result) {
        const fullRange = new vscode.Range(
            editor.document.positionAt(0),
            editor.document.positionAt(code.length)
        );
        editor.edit(builder => builder.replace(fullRange, result));
    }
}

async function improveSelection(): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor) { return; }
    const selection = editor.document.getText(editor.selection);
    if (!selection) {
        vscode.window.showInformationMessage('Select code to improve.');
        return;
    }
    const lang = editor.document.languageId;
    const result = await runQuickTask(
        `Improve this ${lang} code (better naming, structure, performance). Return ONLY the improved code:\n\`\`\`${lang}\n${selection}\n\`\`\``
    );
    if (result && editor) {
        editor.edit(builder => builder.replace(editor.selection, result));
    }
}

// ── API Helpers ─────────────────────────────────────────────

async function runQuickTask(prompt: string): Promise<string | null> {
    if (!serverPort) {
        const choice = await vscode.window.showWarningMessage(
            'Lumen server not running.', 'Start Server', 'Cancel'
        );
        if (choice === 'Start Server') {
            await startServer();
        } else {
            return null;
        }
    }

    try {
        const resp = await fetch(`http://localhost:${serverPort}/v1/chat`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ prompt }),
        });

        const reader = resp.body?.getReader();
        if (!reader) { return null; }

        const decoder = new TextDecoder();
        let buf = '';
        let result = '';

        while (true) {
            const { done, value } = await reader.read();
            if (done) break;
            buf += decoder.decode(value, { stream: true });
            const lines = buf.split('\n');
            buf = lines.pop() || '';
            for (const line of lines) {
                if (!line.startsWith('data: ')) { continue; }
                try {
                    const ev = JSON.parse(line.slice(6));
                    if (ev.kind === 'text') { result += ev.text || ''; }
                } catch (e) { /* skip */ }
            }
        }
        return extractCodeBlock(result);
    } catch (e) {
        vscode.window.showErrorMessage('Lumen request failed: ' + String(e));
        return null;
    }
}

function extractCodeBlock(text: string): string {
    const match = text.match(/```[\w]*\n([\s\S]*?)```/);
    return match ? match[1].trim() : text.trim();
}

// ── Utilities ───────────────────────────────────────────────

function findFreePort(): Promise<number> {
    return new Promise((resolve) => {
        const server = net.createServer();
        server.listen(0, () => {
            const port = (server.address() as net.AddressInfo).port;
            server.close(() => resolve(port));
        });
    });
}

function findLumenConfig(): string | undefined {
    const folders = vscode.workspace.workspaceFolders;
    if (!folders) { return undefined; }
    for (const folder of folders) {
        const configPath = path.join(folder.uri.fsPath, 'lumen.toml');
        if (fs.existsSync(configPath)) { return configPath; }
    }
    // Check home dir
    const homePath = path.join(process.env.HOME || '~', '.config', 'lumen', 'lumen.toml');
    if (fs.existsSync(homePath)) { return homePath; }
    return undefined;
}

function sleep(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
}
