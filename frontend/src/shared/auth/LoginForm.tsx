// v2.0.0-beta.4: freshman added a second import block above the
// existing one without removing duplicates. Consolidated here —
// dropped the unused createFileRoute / Link / redirect (those are
// route-level, not used in this component) and merged the duplicate
// Button / Input / Label / client imports.
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { client } from "@/shared/api/client";
import { useVersion, useActiveSkin } from "@/shared/api/queries";
import { useOIDCAvailable } from "@/shared/auth/useOIDCAvailable";
import { ThemeToggle } from "@/shared/theme/ThemeToggle";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
} from "@/components/ui/card";
import { LoginHeroDisplay } from "@/shared/components/SkinInjector";

// v2.0.0-beta.4: zod schema lives at module load — useTranslation
// is a React hook and can't be called here. Keep the validation
// messages as English literals; react-hook-form will surface
// whatever string we put here. (If we ever need per-locale validation
// messages, the pattern is to construct the schema inside the
// component with useTranslation in scope, but that's overhead we
// don't need today for a single-keystroke required check.)
const loginSchema = z.object({
  username: z.string().min(1, "This field is required"),
  password: z.string().min(1, "This field is required"),
});

type LoginFormValues = z.infer<typeof loginSchema>;

// oidcStartPath is a backend route, NOT a TanStack route. The handler
// returns a 302 to the IdP, so we use full-page navigation
// (window.location.href) rather than the router's Link.
const oidcStartPath = "/api/v1/auth/oidc/start";

export function LoginForm() {
  const navigate = useNavigate();
  const search = useSearch({ from: "/login" });
  const queryClient = useQueryClient();
  const { data: version } = useVersion();
  const { data: oidcAvailable } = useOIDCAvailable();
  const { data: skin, isLoading: skinLoading } = useActiveSkin();
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const { t } = useTranslation("pages");

  const nextPath = typeof search.next === "string" && search.next.startsWith("/") ? search.next : "/files";

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
        toast.error(t("errors.connectionFailed"));
        return;
      }
      if (apiError) {
        setError(apiError.error?.message ?? t("authLogin.signInTitle"));
        return;
      }

      await queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
      navigate({ to: nextPath });
    } catch {
      toast.error(t("errors.connectionFailed"));
    } finally {
      setIsSubmitting(false);
    }
  };

  const onSSOClick = () => {
    // Full browser navigation — the backend returns a 302 to the IdP, so
    // we cannot use TanStack's <Link> (which would just route on the SPA).
    window.location.href = oidcStartPath;
  };

  const loginHero = skin?.loginHero;

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <div className="absolute top-4 right-4">
        <ThemeToggle />
      </div>
      <Card className="w-full max-w-md">
        {loginHero && !skinLoading && (
          <CardHeader className="text-center pb-2">
            <LoginHeroDisplay imageDataUri={loginHero.imageDataUri} tagline={loginHero.tagline} />
          </CardHeader>
        )}
        <CardHeader className="text-center">
          <h1 className="text-2xl font-bold tracking-tight leading-none">{t("authLogin.signInTitle")}</h1>
          <CardDescription>{t("auth.username")} {t("auth.password")}</CardDescription>
        </CardHeader>
        <CardContent>
          {/* OIDC error handling removed in v1.11.0.24 - no longer supported via query param */}

          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="username">{t("auth.username")}</Label>
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
              <Label htmlFor="password">{t("auth.password")}</Label>
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
              {isSubmitting ? t("buttons.saving") : t("authLogin.submitButton")}
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
                {t("authLogin.signInWithSso")}
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
