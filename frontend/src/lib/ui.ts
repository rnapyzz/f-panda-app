// Shared Tailwind utility strings for the plain HTML form/table elements
// used throughout the app, so the same look doesn't get hand-copied into
// every feature page.

export const input =
  'rounded border border-gray-300 bg-white px-2 py-1 text-sm text-gray-900 focus:border-accent focus:outline-none dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100'

export const select = input

export const label = 'inline-flex items-center gap-1.5 text-sm text-gray-700 dark:text-gray-300'

export const buttonPrimary =
  'rounded bg-accent px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50'

export const buttonSecondary =
  'rounded border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800'

export const link = 'text-accent hover:underline'

export const errorText = 'text-sm text-red-600 dark:text-red-400'

export const mutedText = 'text-sm text-gray-500 dark:text-gray-400'

export const fieldset = 'space-y-3 rounded border border-gray-300 p-4 dark:border-gray-600'

export const legend = 'px-1 text-sm font-semibold text-gray-900 dark:text-gray-100'

export const table = 'min-w-full border-collapse text-sm'

export const th =
  'whitespace-nowrap border border-gray-300 bg-gray-50 px-3 py-1.5 text-left font-semibold text-gray-700 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-200'

export const td = 'whitespace-nowrap border border-gray-300 px-3 py-1.5 dark:border-gray-600'

export const tdRight = `${td} text-right`

export const pageHeading = 'mb-4 text-xl font-semibold text-gray-900 dark:text-gray-100'

export const sectionHeading = 'mb-2 mt-6 text-base font-semibold text-gray-900 dark:text-gray-100'
