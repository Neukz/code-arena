export const difficulties = ["easy", "medium", "hard"] as const;

export type Difficulty = (typeof difficulties)[number];

export function difficultyClass(difficulty: string): string {
  switch (difficulty) {
    case "easy":
      return "text-green-700 bg-green-50 dark:bg-green-950 dark:text-green-300";
    case "medium":
      return "text-yellow-700 bg-yellow-50 dark:bg-yellow-950 dark:text-yellow-300";
    case "hard":
      return "text-red-700 bg-red-50 dark:bg-red-950 dark:text-red-300";
    default:
      return "text-zinc-700 bg-zinc-100 dark:bg-zinc-800 dark:text-zinc-300";
  }
}
