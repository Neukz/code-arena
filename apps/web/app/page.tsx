import { listProblems } from "@/lib/api";
import { ProblemList } from "@/components/problem-list";

export const dynamic = "force-dynamic";

export default async function HomePage() {
  const problems = await listProblems();

  return (
    <main className="mx-auto w-full max-w-6xl flex-1 px-6 py-12">
      <div className="mb-8">
        <h1 className="text-3xl font-bold tracking-tight text-zinc-900 dark:text-zinc-50">
          Problems
        </h1>
        <p className="mt-2 text-zinc-600 dark:text-zinc-400">
          Pick a challenge, write your solution, and run it against the test
          cases.
        </p>
      </div>
      <ProblemList problems={problems} />
    </main>
  );
}
