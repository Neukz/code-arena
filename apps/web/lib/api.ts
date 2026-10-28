import type {
  CreateSubmissionRequest,
  CreateSubmissionResponse,
  Language,
  ListLanguagesResponse,
  ListProblemsResponse,
  ProblemDetail,
  ProblemListItem,
} from "@/types";

const API_BASE = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

export class ApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly code: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function parseError(response: Response): Promise<ApiError> {
  let code = "unknown_error";
  let message = `Request failed with status ${response.status}`;
  try {
    const body = (await response.json()) as {
      error?: { code?: string; message?: string };
    };
    code = body.error?.code ?? code;
    message = body.error?.message ?? message;
  } catch {
    // Fallback to status text if body is not JSON.
  }
  return new ApiError(message, response.status, code);
}

async function get<T>(path: string): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: { accept: "application/json" },
  });
  if (!response.ok) {
    throw await parseError(response);
  }
  return (await response.json()) as T;
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    method: "POST",
    headers: {
      accept: "application/json",
      "content-type": "application/json",
    },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    throw await parseError(response);
  }
  return (await response.json()) as T;
}

export async function listProblems(): Promise<ProblemListItem[]> {
  const body = await get<ListProblemsResponse>("/api/v1/problems");
  return body.data ?? [];
}

export async function getProblem(slug: string): Promise<ProblemDetail> {
  return get<ProblemDetail>(`/api/v1/problems/${encodeURIComponent(slug)}`);
}

export async function listLanguages(): Promise<Language[]> {
  const body = await get<ListLanguagesResponse>("/api/v1/languages");
  return body.data ?? [];
}

export async function createSubmission(
  request: CreateSubmissionRequest,
): Promise<CreateSubmissionResponse> {
  return post<CreateSubmissionResponse>("/api/v1/submissions", request);
}
