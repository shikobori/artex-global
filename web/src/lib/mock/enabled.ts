// Mock switch: a build-time public variable (the browser can read only NEXT_PUBLIC_ variables).
// Set NEXT_PUBLIC_MOCK=1 on Vercel to run the entire site with mocks and no backend.
export const MOCK = process.env.NEXT_PUBLIC_MOCK === "1";
