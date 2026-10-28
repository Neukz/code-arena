import { notFound } from "next/navigation";
import { ApiError, getProblem, listLanguages } from "@/lib/api";
import { ProblemDetail } from "@/components/problem-detail";
import type { Language, ProblemDetail as ProblemDetailType } from "@/types";

export const dynamic = "force-dynamic";

export default async function ProblemPage({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = await params;

  let problem: ProblemDetailType;
  let languages: Language[];
  try {
    [problem, languages] = await Promise.all([
      getProblem(slug),
      listLanguages(),
    ]);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      notFound();
    }
    throw error;
  }

  return (
    <main className="mx-auto flex w-full max-w-7xl flex-1 flex-col px-6 py-8">
      <ProblemDetail problem={problem} languages={languages} />
    </main>
  );
}
