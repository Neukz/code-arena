"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { CodeEditor } from "./code-editor";
import { LanguageSelect } from "./language-select";
import { createSubmission } from "@/lib/api";
import type { Language, ProblemDetail } from "@/types";

export function SubmitForm({
  problem,
  languages,
}: {
  problem: ProblemDetail;
  languages: Language[];
}) {
  const router = useRouter();
  const [languageId, setLanguageId] = useState<string>(languages[0]?.id ?? "");
  const [code, setCode] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!languageId || isSubmitting) return;

    setIsSubmitting(true);
    setError(null);

    try {
      const response = await createSubmission({
        problem_id: problem.id,
        language_id: languageId,
        source_code: code,
      });
      router.push(`/submissions/${response.submission_id}`);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Submission failed";
      setError(message);
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="flex min-h-0 flex-1 flex-col rounded-lg border border-zinc-200 bg-zinc-950 dark:border-zinc-800"
    >
      <div className="flex items-center justify-between border-b border-zinc-800 p-3">
        <LanguageSelect
          languages={languages}
          value={languageId}
          onChange={setLanguageId}
        />
        <button
          type="submit"
          disabled={isSubmitting || !languageId}
          className="rounded-md bg-zinc-100 px-4 py-2 text-sm font-medium text-zinc-900 transition hover:bg-white disabled:cursor-not-allowed disabled:opacity-50"
        >
          {isSubmitting ? "Submitting..." : "Submit"}
        </button>
      </div>

      <div className="flex min-h-0 flex-1 flex-col">
        <CodeEditor
          language={monacoLanguage(languageId, languages)}
          value={code}
          onChange={setCode}
        />
      </div>

      {error && (
        <div className="border-t border-zinc-800 bg-red-950/30 p-3 text-sm text-red-200">
          {error}
        </div>
      )}
    </form>
  );
}

function monacoLanguage(languageId: string, languages: Language[]): string {
  const lang = languages.find((l) => l.id === languageId);
  switch (lang?.id) {
    case "go":
      return "go";
    case "javascript":
      return "javascript";
    case "python":
      return "python";
    default:
      return "plaintext";
  }
}
