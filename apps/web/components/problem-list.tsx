"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { difficultyClass, type Difficulty } from "@/lib/difficulty";
import type { ProblemListItem } from "@/types";

export function ProblemList({ problems }: { problems: ProblemListItem[] }) {
  const [difficulty, setDifficulty] = useState<Difficulty | "all">("all");
  const [query, setQuery] = useState("");

  const filtered = useMemo(() => {
    const lower = query.trim().toLowerCase();
    return problems.filter((p) => {
      const matchesDifficulty = difficulty === "all" || p.difficulty === difficulty;
      const matchesQuery = !lower || p.title.toLowerCase().includes(lower);
      return matchesDifficulty && matchesQuery;
    });
  }, [problems, difficulty, query]);

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
        <label className="flex flex-col gap-1 text-sm font-medium">
          Search
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter by title"
            className="rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm focus:outline-hidden focus:ring-2 focus:ring-zinc-950 dark:border-zinc-700 dark:bg-zinc-900"
          />
        </label>
        <label className="flex flex-col gap-1 text-sm font-medium">
          Difficulty
          <select
            value={difficulty}
            onChange={(e) => setDifficulty(e.target.value as Difficulty | "all")}
            className="rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm focus:outline-hidden focus:ring-2 focus:ring-zinc-950 dark:border-zinc-700 dark:bg-zinc-900"
          >
            <option value="all">All</option>
            <option value="easy">Easy</option>
            <option value="medium">Medium</option>
            <option value="hard">Hard</option>
          </select>
        </label>
      </div>

      {filtered.length === 0 ? (
        <p className="text-zinc-600 dark:text-zinc-400">No problems match your filters.</p>
      ) : (
        <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {filtered.map((problem) => (
            <li key={problem.id}>
              <Link
                href={`/problems/${encodeURIComponent(problem.slug)}`}
                className="group block rounded-lg border border-zinc-200 bg-white p-5 shadow-xs transition hover:border-zinc-300 hover:shadow-sm dark:border-zinc-800 dark:bg-zinc-900 dark:hover:border-zinc-700"
              >
                <div className="flex items-start justify-between gap-3">
                  <h2 className="font-semibold text-zinc-900 group-hover:underline dark:text-zinc-50">
                    {problem.title}
                  </h2>
                  <span
                    className={`shrink-0 rounded-full px-2 py-0.5 text-xs font-medium capitalize ${difficultyClass(problem.difficulty)}`}
                  >
                    {problem.difficulty}
                  </span>
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
