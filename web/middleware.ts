import { NextResponse, type NextRequest } from "next/server";

/**
 * Session gate. The JWT lives in an httpOnly cookie the browser cannot read,
 * so the edge is the only place a redirect can happen before paint — checking
 * `["me"]` client-side would flash the protected screen first.
 *
 * Presence of the cookie is all we check: validating the signature needs
 * JWT_SECRET, which belongs to the Go API. A forged cookie therefore reaches
 * the app shell, where every API call still returns 401.
 */
const SESSION_COOKIE = "fg_session";

export function middleware(req: NextRequest) {
  const signedIn = req.cookies.has(SESSION_COOKIE);
  const isLogin = req.nextUrl.pathname === "/login";

  if (!signedIn && !isLogin) {
    const url = req.nextUrl.clone();
    url.pathname = "/login";
    url.search = "";
    return NextResponse.redirect(url);
  }

  if (signedIn && isLogin) {
    const url = req.nextUrl.clone();
    url.pathname = "/workflows";
    url.search = "";
    return NextResponse.redirect(url);
  }

  return NextResponse.next();
}

export const config = {
  // Everything except framework internals, the proxied API, the public webhook
  // ingress, and any request that looks like a static file (has an extension).
  matcher: ["/((?!_next/|api/|webhook/|favicon\\.ico|.*\\.[^/]+$).*)"],
};
