import * as React from "react"
import { Input as InputPrimitive } from "@base-ui/react/input"

import { cn } from "@/lib/utils"

function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <InputPrimitive
      type={type}
      data-slot="input"
      // v1.11.0.17 mobile audit — h-8 (32px) was below the WCAG 2.5.5
      // / iOS HIG 44×44 tap-target floor on phones. min-h-[44px] floor
      // on touch viewports (<sm) preserves the dense 32px desktop visual
      // while making the touch target reach all the way down to where
      // a thumb naturally lands. Tested against iPhone SE/14, Android
      // narrow, iPad Mini.
      className={cn(
        "h-8 min-h-[44px] sm:min-h-0 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none file:inline-flex file:h-6 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:cursor-not-allowed disabled:bg-input/50 disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 md:text-sm dark:bg-input/30 dark:disabled:bg-input/80 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40",
        className
      )}
      {...props}
    />
  )
}

export { Input }
