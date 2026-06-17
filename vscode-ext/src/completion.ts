import * as vscode from 'vscode';

/**
 * InlineCompletionProvider that queries Lumen for code suggestions.
 * Uses a simple approach: send the file prefix (everything before cursor)
 * and ask the model to complete it. Debounced to avoid hammering the API.
 */
export class LumenCompletionProvider implements vscode.InlineCompletionItemProvider {
    private portFn: () => number;
    private debounceTimer: ReturnType<typeof setTimeout> | null = null;
    private lastResult: string = '';

    constructor(portFn: () => number) {
        this.portFn = portFn;
    }

    async provideInlineCompletionItems(
        document: vscode.TextDocument,
        position: vscode.Position,
        _context: vscode.InlineCompletionContext,
        _token: vscode.CancellationToken
    ): Promise<vscode.InlineCompletionItem[]> {
        const port = this.portFn();
        if (!port) { return []; }

        const config = vscode.workspace.getConfiguration('lumen');
        if (!config.get<boolean>('completion.enabled', true)) { return []; }

        // Only trigger at end of line or after a pause
        const line = document.lineAt(position.line);
        const textBeforeCursor = line.text.substring(0, position.character);

        // Don't complete on empty lines or very short prefixes
        if (textBeforeCursor.trim().length < 3) { return []; }

        // Don't complete in comments or strings (simple heuristic)
        if (textBeforeCursor.trimStart().startsWith('//') ||
            textBeforeCursor.trimStart().startsWith('#') ||
            textBeforeCursor.trimStart().startsWith('/*')) {
            return [];
        }

        // Debounce
        const debounceMs = config.get<number>('completion.debounceMs', 500);
        return new Promise((resolve) => {
            if (this.debounceTimer) { clearTimeout(this.debounceTimer); }
            this.debounceTimer = setTimeout(async () => {
                const items = await this.fetchCompletion(document, position, port);
                resolve(items);
            }, debounceMs);
        });
    }

    private async fetchCompletion(
        document: vscode.TextDocument,
        position: vscode.Position,
        port: number
    ): Promise<vscode.InlineCompletionItem[]> {
        try {
            // Build context: current file prefix + surrounding lines
            const context = this.buildContext(document, position);

            const prompt = [
                `You are an inline code completion engine. Complete the code at <cursor> position.`,
                `File: ${document.fileName}`,
                `Language: ${document.languageId}`,
                ``,
                `Return ONLY the completion text (no explanation, no markdown fences).`,
                `The completion should be a natural continuation of the existing code.`,
                `Keep it concise — usually 1-2 lines, sometimes up to 5 lines for blocks.`,
                ``,
                `Code context:`,
                '```' + document.languageId,
                context,
                '```',
            ].join('\n');

            const controller = new AbortController();
            const timeout = setTimeout(() => controller.abort(), 3000);

            const resp = await fetch(`http://localhost:${port}/v1/chat`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ prompt }),
                signal: controller.signal,
            });

            clearTimeout(timeout);

            if (!resp.ok) { return []; }

            const reader = resp.body?.getReader();
            if (!reader) { return []; }

            const decoder = new TextDecoder();
            let buf = '';
            let result = '';

            while (true) {
                const { done, value } = await reader.read();
                if (done) { break; }
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

            // Clean result: remove any markdown fences, leading/trailing whitespace
            result = result
                .replace(/```[\w]*\n?/g, '')
                .replace(/^\s+|\s+$/g, '');

            if (!result || result.length < 2) { return []; }

            // Don't repeat the same completion
            if (result === this.lastResult) { return []; }
            this.lastResult = result;

            return [new vscode.InlineCompletionItem(result)];
        } catch (e) {
            // Silently fail — completions are best-effort
            return [];
        }
    }

    private buildContext(document: vscode.TextDocument, position: vscode.Position): string {
        const lines: string[] = [];

        // 10 lines before cursor
        const startLine = Math.max(0, position.line - 10);
        for (let i = startLine; i < position.line; i++) {
            lines.push(document.lineAt(i).text);
        }

        // Current line up to cursor
        const currentLine = document.lineAt(position.line).text;
        lines.push(currentLine.substring(0, position.character) + '<cursor>');

        // 3 lines after cursor (for context)
        const endLine = Math.min(document.lineCount - 1, position.line + 3);
        for (let i = position.line + 1; i <= endLine; i++) {
            lines.push(document.lineAt(i).text);
        }

        return lines.join('\n');
    }
}
