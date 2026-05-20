import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { client } from "@/shared/api/client";
import { useVersion } from "@/shared/api/queries";
import { useOIDCAvailable } from "@/shared/auth/useOIDCAvailable";
import { ThemeToggle } from "@/shared/theme/ThemeToggle";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

const loginSchema = z.object({
  username: z.string().min(1, "Username is required"),
  password: z.string().min(1, "Password is required"),
});

type LoginFormValues = z.infer<typeof loginSchema>;

// oidcStartPath is a backend route, NOT a TanStack route. The handler
// returns a 302 to the IdP, so we use full-page navigation
// (window.location.href) rather than the router's Link.
const oidcStartPath = "/api/v1/auth/oidc/start";

// oidcErrorMessages translates the codes the backend returns from
// internal/api/auth_oidc.go into user-facing copy. The backend currently
// renders those errors as JSON 4xx responses, so users only see them
// here if the operator (or a future revision of the callback) routes the
// browser back to /admin/login?error=<code>.
const oidcErrorMessages: Record<string, string> = {
  OIDC_STATE_MISSING: "Sign-in session expired. Please try again.",
  OIDC_STATE_INVALID: "Sign-in session was malformed. Please try again.",
  OIDC_STATE_MISMATCH: "Sign-in could not be verified. Please try again.",
  OIDC_CODE_MISSING: "Identity provider did not return an authorization code.",
  OIDC_EXCHANGE_FAILED: "Could not exchange the authorization code with the identity provider.",
  OIDC_ID_TOKEN_MISSING: "Identity provider response was missing the ID token.",
  OIDC_ID_TOKEN_INVALID: "Identity provider's ID token failed verification.",
  USER_NOT_PROVISIONED: "Your account isn't provisioned. Contact your administrator.",
  OIDC_NOT_CONFIGURED: "Single sign-on is not configured on this server.",
};

function oidcErrorMessage(code: string): string {
  return oidcErrorMessages[code] ?? `Sign-in failed (${code}).`;
}

export function LoginForm() {
  const navigate = useNavigate();
  const search = useSearch({ from: "/admin/login" });
  const queryClient = useQueryClient();
  const { data: version } = useVersion();
  const { data: oidcAvailable } = useOIDCAvailable();
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const nextPath = typeof search.next === "string" ? search.next : "/";
  const oidcError = typeof search.error === "string" ? search.error : null;

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<LoginFormValues>({
    resolver: zodResolver(loginSchema),
  });

  const onSubmit = async (values: LoginFormValues) => {
    setError(null);
    setIsSubmitting(true);

    try {
      const { error: apiError, response } = await client.POST("/auth/login", {
        body: { username: values.username, password: values.password },
      });

      if (response.status === 401) {
        setError("Invalid credentials");
        return;
      }
      if (response.status >= 500) {
        toast.error("Something went wrong, try again");
        return;
      }
      if (apiError) {
        setError(apiError.error?.message ?? "Login failed. Please try again.");
        return;
      }

      await queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
      navigate({ to: nextPath });
    } catch {
      toast.error("Something went wrong, try again");
    } finally {
      setIsSubmitting(false);
    }
  };

  const onSSOClick = () => {
    // Full browser navigation — the backend returns a 302 to the IdP, so
    // we cannot use TanStack's <Link> (which would just route on the SPA).
    window.location.href = oidcStartPath;
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <div className="absolute top-4 right-4">
        <ThemeToggle />
      </div>
      <Card className="w-full max-w-md">
        <CardHeader className="text-center">
          <CardTitle className="text-2xl font-bold">Sign in to basement</CardTitle>
          <CardDescription>Enter your credentials to access the admin dashboard</CardDescription>
        </CardHeader>
        <CardContent>
          {oidcError && (
            <div
              role="alert"
              className="mb-4 rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive"
            >
              {oidcErrorMessage(oidcError)}
            </div>
          )}

          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="username">Username</Label>
              <Input
                id="username"
                type="text"
                placeholder="admin"
                autoComplete="username"
                {...register("username")}
                disabled={isSubmitting}
              />
              {errors.username && (
                <p className="text-sm text-destructive">{errors.username.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                placeholder="••••••••"
                autoComplete="current-password"
                {...register("password")}
                disabled={isSubmitting}
              />
              {errors.password && (
                <p className="text-sm text-destructive">{errors.password.message}</p>
              )}
            </div>

            {error && <p className="text-sm text-destructive">{error}</p>}

            <Button type="submit" className="w-full" disabled={isSubmitting}>
              {isSubmitting ? "Signing in..." : "Sign in"}
            </Button>
          </form>

          {oidcAvailable && (
            <>
              <div className="my-4 flex items-center gap-3">
                <div className="h-px flex-1 bg-border" />
                <span className="text-xs text-muted-foreground">or</span>
                <div className="h-px flex-1 bg-border" />
              </div>
              <Button
                type="button"
                variant="outline"
                className="w-full"
                onClick={onSSOClick}
                data-testid="sso-button"
              >
                Sign in with SSO
              </Button>
            </>
          )}

          {version && (
            <p
              className="mt-6 text-center text-xs text-muted-foreground/60"
              title={`commit ${version.commit?.slice(0, 7) ?? "unknown"} · built ${version.builtAt}`}
            >
              {version.version}
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
