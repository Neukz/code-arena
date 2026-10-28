"use client";

import Editor from "@monaco-editor/react";

export function CodeEditor({
  language,
  value,
  onChange,
}: {
  language: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <Editor
      language={language}
      value={value}
      onChange={(v) => onChange(v ?? "")}
      theme="vs-dark"
      options={{
        minimap: { enabled: false },
        fontSize: 14,
        automaticLayout: true,
        tabSize: 2,
        insertSpaces: true,
      }}
      className="flex-1"
    />
  );
}
