import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useOrgCapabilities, isSignupEnabled } from "@/shared/api/queries";

export const Route = createFileRoute("/signup")({
  component: SignupPage,
});

function SignupPage() {
  const { t } = useTranslation("pages");
  const { data: orgCaps, isLoading } = useOrgCapabilities();
  const [redirected, setRedirected] = useState(false);

  useEffect(() => {
    if (orgCaps && !isSignupEnabled(orgCaps.signupMode) && !redirected) {
      // Redirect back to login if signup is disabled
      window.location.href = "/login";
      setRedirected(true);
    }
  }, [orgCaps, redirected]);

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background p-4">
        <Card className="w-full max-w-md">
          <CardContent className="pt-6 text-center">
            {t("states.loading")}
          </CardContent>
        </Card>
      </div>
    );
  }

  if (!orgCaps || redirected) {
    return null;
  }

  const isDisabled = !isSignupEnabled(orgCaps.signupMode);

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-md">
        <CardHeader className="text-center">
          <CardTitle>{t("authSignup.pageTitle")}</CardTitle>
          <CardDescription>{t("authSignup.pageSubtitle")}</CardDescription>
        </CardHeader>
        <CardContent>
          {isDisabled ? (
            <div className="space-y-4 text-center">
              <p className="text-sm text-muted-foreground">
                {t("authSignup.disabledMessage")}
              </p>
              <Link to="/login">
                <Button variant="outline" className="w-full">
                  {t("authSignup.backToLogin")}
                </Button>
              </Link>
            </div>
          ) : (
            <div className="space-y-4 text-center">
              <p className="text-sm text-muted-foreground">
                {t("authSignup.enabledMessage")}
              </p>
              <Link to="/login">
                <Button variant="outline" className="w-full">
                  {t("authSignup.backToLogin")}
                </Button>
              </Link>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
