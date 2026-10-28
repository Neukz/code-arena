"use client";

import Link from "next/link";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { SubmitForm } from "./submit-form";
import { difficultyClass } from "@/lib/difficulty";
import type { Language, ProblemDetail as ProblemDetailType } from "@/types";

export function ProblemDetail({
  problem,
  languages,
}: {
  problem: ProblemDetailType;
  languages: Language[];
}) {
  return (
    <div className="grid flex-1 gap-8 lg:grid-cols-2">
      <section className="space-y-6">
        <div className="flex items-center gap-3">
          <Link
            href="/"
            className="text-sm font-medium text-zinc-600 hover:text-zinc-900 dark:text-zinc-400 dark:hover:text-zinc-50"
          >
            &larr; Back to problems
          </Link>
          <span className="text-zinc-300 dark:text-zinc-700">|</span>
          <span
            className={`rounded-full px-2 py-0.5 text-xs font-medium capitalize ${difficultyClass(problem.difficulty)}`}
          >
            {problem.difficulty}
          </span>
        </div>

        <h1 className="text-3xl font-bold tracking-tight text-zinc-900 dark:text-zinc-50">
          {problem.title}
        </h1>

        <article className="prose prose-zinc max-w-none dark:prose-invert">
          <ReactMarkdown remarkPlugins={[remarkGfm]}>
            {problem.description}
          </ReactMarkdown>
        </article>

        {problem.test_cases.length > 0 && (
          <div className="space-y-4">
            <h2 className="text-lg font-semibold text-zinc-900 dark:text-zinc-50">
              Sample test cases
            </h2>
            <ul className="space-y-4">
              {problem.test_cases.map((tc) => (
                <li
                  key={tc.id}
                  className="rounded-lg border border-zinc-200 bg-zinc-50 p-4 dark:border-zinc-800 dark:bg-zinc-900"
                >
                  <p className="text-sm font-medium text-zinc-900 dark:text-zinc-50">
                    {tc.name}
                  </p>
                  <div className="mt-2 grid gap-3 sm:grid-cols-2">
                    <pre className="overflow-x-auto rounded bg-white p-3 text-xs dark:bg-black">
                      <code>
                        <strong>Input:</strong>
                        {"\n"}
                        {tc.input}
                      </code>
                    </pre>
                    <pre className="overflow-x-auto rounded bg-white p-3 text-xs dark:bg-black">
                      <code>
                        <strong>Expected:</strong>
                        {"\n"}
                        {tc.expected_output}
                      </code>
                    </pre>
                  </div>
                </li>
              ))}
            </ul>
          </div>
        )}
      </section>

      <SubmitForm problem={problem} languages={languages} />
    </div>
  );
}
