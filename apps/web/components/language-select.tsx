"use client";

import type { Language } from "@/types";

export function LanguageSelect({
  languages,
  value,
  onChange,
}: {
  languages: Language[];
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="flex items-center gap-2 text-sm font-medium text-zinc-200">
      Language
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="rounded-md border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-sm text-zinc-100 focus:outline-hidden focus:ring-2 focus:ring-zinc-400"
      >
        {languages.map((lang) => (
          <option key={lang.id} value={lang.id}>
            {lang.display_name}
          </option>
        ))}
      </select>
    </label>
  );
}
