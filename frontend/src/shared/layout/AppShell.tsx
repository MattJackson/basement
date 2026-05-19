import type { ReactNode } from "react";
import { Outlet } from "@tanstack/react-router";
import { HealthPill } from "@/shared/ui/HealthPill";
import { UserMenu } from "@/shared/ui/UserMenu";
import { ThemeToggle } from "@/shared/theme/ThemeToggle";
import { useNodes } from "@/shared/api/queries";

interface AppShellProps {
  children?: ReactNode;
}

/**
 * AppShell renders the authed admin chrome (sidebar + topbar) around
 * either explicit children OR the TanStack Router <Outlet/> for the
 * current matched child route.
 */
export function AppShell({ children }: AppShellProps): ReactNode {
  const { data: nodes, isLoading: loadingNodes } = useNodes();
  
  const totalNodes = nodes?.length ?? 0;
  const healthyNodes = nodes?.filter(n => n.address).length ?? 0;

  let healthStatus: "healthy" | "degraded" | "unavailable" = "unavailable";
  if (!loadingNodes) {
    if (totalNodes === 0) {
      healthStatus = "unavailable";
    } else if (healthyNodes === totalNodes) {
      healthStatus = "healthy";
    } else {
      healthStatus = "degraded";
    }
  }

  const navItems = [
    { label: "Dashboard", href: "/admin" },
    { label: "Cluster", children: [
      { label: "Nodes", href: "/admin/cluster/nodes" },
      { label: "Layout", href: "/admin/cluster/layout" },
    ]},
    { label: "Buckets", href: "/admin/buckets" },
    { label: "Keys", href: "/admin/keys" },
    { label: "Diagnostics", href: "/admin/diagnostics" },
  ];

  return (
    <div className="flex min-h-screen bg-background">
      <aside className="fixed left-0 top-0 h-full w-60 border-r bg-card flex flex-col z-40">
        <div className="p-4 border-b">
          <a href="/admin" className="text-lg font-bold hover:opacity-80 transition-opacity">
            basement
          </a>
        </div>
        
        <nav className="flex-1 p-4 space-y-1">
          {navItems.map((item) => {
            if (item.children) {
              return (
                <div key={item.label} className="space-y-1">
                  <div className="px-3 py-2 text-sm font-medium opacity-60 uppercase tracking-wider">
                    {item.label}
                  </div>
                  {item.children.map((child) => (
                    <a
                      key={child.href}
                      href={child.href}
                      className="block px-3 py-2 rounded-md text-sm hover:bg-muted transition-colors"
                    >
                      {child.label}
                    </a>
                  ))}
                </div>
              );
            }
            return (
              <a
                key={item.href}
                href={item.href}
                className="block px-3 py-2 rounded-md text-sm hover:bg-muted transition-colors"
              >
                {item.label}
              </a>
            );
          })}
        </nav>

        <div className="p-4 border-t text-xs opacity-60 space-y-1">
          <span>v0.1.0-dev</span>
          <a href="https://github.com" target="_blank" rel="noopener noreferrer" className="hover:underline block">
            GitHub
          </a>
        </div>
      </aside>

      <main className="flex-1 ml-60 flex flex-col min-h-screen">
        <header className="fixed right-0 top-0 h-14 w-full border-b bg-card z-30 px-6 flex items-center justify-between">
          <div />
          <div className="flex items-center gap-2">
            <HealthPill status={healthStatus} />
            <ThemeToggle />
            <UserMenu />
          </div>
        </header>

        <div className="flex-1 pt-14 p-8 max-w-[1280px] mx-auto w-full">
          {children ?? <Outlet />}
        </div>
      </main>
    </div>
  );
}
