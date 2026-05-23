import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useOrgCapabilities, isSignupEnabled } from "@/shared/api/queries";

export const Route = createFileRoute("/signup")({
  component: SignupPage,
});

function SignupPage() {
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
            Loading...
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
          <CardTitle>Sign Up</CardTitle>
          <CardDescription>Create a new account</CardDescription>
        </CardHeader>
        <CardContent>
          {isDisabled ? (
            <div className="space-y-4 text-center">
              <p className="text-sm text-muted-foreground">
                Sign-up is currently disabled for this organization. Please contact your administrator.
              </p>
              <Link to="/login">
                <Button variant="outline" className="w-full">
                  Back to Login
                </Button>
              </Link>
            </div>
          ) : (
            <div className="space-y-4 text-center">
              <p className="text-sm text-muted-foreground">
                Sign-up is enabled. Registration functionality will be available soon.
              </p>
              <Link to="/login">
                <Button variant="outline" className="w-full">
                  Back to Login
                </Button>
              </Link>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
