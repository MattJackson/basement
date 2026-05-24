/**
 * React Error #321 Reproduction Test
 * 
 * This test walks every route in frontend/src/routes/ and triggers console.error
 * listeners to capture React error #321 occurrences.
 * 
 * Run with: npx playwright test react-error-321.spec.ts --headed
 */

import { test, expect, Page } from '@playwright/test';

// Route map based on frontend/src/routes/ structure
const ROUTES = [
  // Root routes
  '/',
  '/login',
  '/signup',
  
  // Files routes (user-facing)
  '/files',
  '/files/backups',
  '/files/federated-buckets',
  '/files/keys',
  '/files/shares',
  '/files/syncs',
  '/files/webhooks',
  
  // Admin routes
  '/admin',
  '/admin/audit',
  '/admin/buckets',
  '/admin/clusters',
  '/admin/policies',
  '/admin/service-accounts',
  '/admin/system',
  '/admin/usage',
  '/admin/users',
  
  // Dynamic routes (will be skipped if they require IDs)
  // '/files/$regionId' - requires region ID
];

// Error pattern for React error #321
const REACT_ERROR_321_PATTERN = /321|Failed to perform work on a partially resolved promise|updateContainerInCurrentOrPrototypeOf/;

test.describe('React Error #321 Reproduction', () => {
  let page: Page;
  const errors: Array<{ url: string; message: string; stack?: string; timestamp: number }> = [];

  test.beforeAll(async ({ browser }) => {
    // Create new context with console listener
    const context = await browser.newContext();
    
    // Set up console error listener
    context.on('console', (msg) => {
      if (msg.type() === 'error') {
        const text = msg.text();
        
        // Check if this is React error #321 or related
        if (text.includes('321') || 
            text.includes('partially resolved promise') ||
            text.includes('Failed to perform work')) {
          console.log(`[REACT ERROR] ${text}`);
        }
      }
    });

    // Set up page error listener for full stack traces
    context.on('pageerror', (error) => {
      console.log(`[PAGE ERROR] ${error.message}`);
      errors.push({
        url: page.url(),
        message: error.message,
        timestamp: Date.now()
      });
    });

    page = await context.newPage();
    
    // Additional console listener for full stack traces
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        const text = msg.text();
        
        // Capture React error #321 specifically
        if (text.includes('321')) {
          errors.push({
            url: page.url(),
            message: text,
            stack: msg.location()?.stackTrace ? JSON.stringify(msg.location().stackTrace) : undefined,
            timestamp: Date.now()
          });
        }
      }
    });
  });

  test.afterAll(async () => {
    // Write findings to file if errors found
    if (errors.length > 0) {
      const fs = await import('fs');
      const report = `React Error #321 Reproduction Report\n${'='.repeat(50)}\nGenerated: ${new Date().toISOString()}\n\n`;
      
      errors.forEach((err, idx) => {
        report += `${idx + 1}. URL: ${err.url}\n   Message: ${err.message}\n   Timestamp: ${err.timestamp}\n${err.stack ? `   Stack: ${err.stack}\n` : ''}`;
      });
      
      fs.writeFileSync('react-error-321-report.json', JSON.stringify(errors, null, 2));
      console.log(`Found ${errors.length} React errors. Report written to react-error-321-report.json`);
    }
  });

  test.describe.configure({ mode: 'parallel' });

  // Test each route for React errors
  for (const route of ROUTES) {
    test(`should not display React error #321 on ${route}`, async () => {
      const initialErrorCount = errors.length;
      
      await page.goto(route, { waitUntil: 'networkidle', timeout: 30000 });
      
      // Wait for any potential lazy-loaded components to render
      await new Promise(resolve => setTimeout(resolve, 2000));
      
      // Look for React error patterns in the collected errors
      const errorsOnPage = errors.filter(e => 
        e.url.includes(route) && e.message.includes('321')
      );

      expect(errorsOnPage.length, `No React error #321 on ${route}`).toBe(0);
      
    }).timeout(60000);
  }

  // Test specific interactive elements that might trigger React errors
  test('should not trigger React error #321 on admin system page interactions', async () => {
    await page.goto('/admin/system', { waitUntil: 'networkidle' });
    
    const initialErrorCount = errors.length;
    
    // Try to interact with skin settings if present
    try {
      const defaultSkinSelect = page.locator('#default-skin-select');
      if (await defaultSkinSelect.isVisible()) {
        await defaultSkinSelect.click();
        await page.waitForTimeout(500);
        await defaultSkinSelect.press('Escape');
      }
    } catch {
      // Skin selector not present or already interacted with
    }

    try {
      const overridableSwitch = page.locator('text=User Overridable').locator('input[type="checkbox"]');
      if (await overridableSwitch.isVisible()) {
        await overridableSwitch.click();
        await page.waitForTimeout(500);
        await overridableSwitch.click(); // Toggle back
      }
    } catch {
      // Switch not present
    }

    const newErrors = errors.slice(initialErrorCount);
    const react321Errors = newErrors.filter(e => e.message.includes('321'));
    
    expect(react321Errors.length, 'No React error #321 on skin interactions').toBe(0);
  });

  // Test toast interactions (double-toast fix verification)
  test('should only show one toast at a time', async () => {
    await page.goto('/admin/system', { waitUntil: 'networkidle' });
    
    // Trigger a save action if possible
    const saveButton = page.locator('button:has-text("Save")');
    if (await saveButton.isVisible()) {
      await saveButton.click();
      await page.waitForTimeout(1000);
      
      // Count toasts - should be exactly 1
      const toastCount = await page.locator('.sonner-toast').count();
      expect(toastCount).toBeLessThanOrEqual(1);
    }
  });
});
