/**
 * Wire P3.6 interactive input filters for Option A (full re-render) reactivity.
 *
 * The backend renders each control inside a wrapper element carrying
 * `data-vibe-input="<variable name>"`. After every preview render this function
 * attaches change listeners to the rendered controls. When the user changes a
 * control it reads the control's current value, applies the round-trip mapping
 * rules below, and invokes `onChange(name, value)` so the host can update its
 * reactive filter-params map and re-call RenderPageContent with it.
 *
 * Round-trip mapping (must mirror studio/app.go renderInput):
 *   - dropdown      <select>          -> selected option value
 *   - button_group  [data-vibe-value] -> clicked button's data-vibe-value
 *   - slider        <input type=range>-> .value
 *   - text_input    <input type=text> -> .value ("" => param removed/unfiltered)
 *   - toggle        <input type=checkbox> -> data-vibe-checked-value when checked,
 *                   data-vibe-unchecked-value (default "") when unchecked.
 *                   An empty mapped value removes the param (=> unfiltered).
 *
 * `onChange` receives the empty string for "remove this param"; the host should
 * delete the key in that case so an absent variable means unfiltered, matching
 * the backend {#if var} semantics.
 */
export type InputFilterChange = (name: string, value: string) => void

export function wireInputFilters(
  container: HTMLElement | null,
  onChange: InputFilterChange,
): void {
  if (!container) return

  const wrappers = container.querySelectorAll<HTMLElement>('[data-vibe-input]')
  wrappers.forEach((wrapper) => {
    const name = wrapper.getAttribute('data-vibe-input')
    if (!name) return

    // dropdown
    const select = wrapper.querySelector<HTMLSelectElement>('select.vibe-dropdown')
    if (select) {
      select.addEventListener('change', () => onChange(name, select.value))
    }

    // slider
    const slider = wrapper.querySelector<HTMLInputElement>('input.vibe-slider')
    if (slider) {
      slider.addEventListener('input', () => onChange(name, slider.value))
    }

    // text_input ("" => remove param)
    const text = wrapper.querySelector<HTMLInputElement>('input.vibe-text-input')
    if (text) {
      text.addEventListener('input', () => onChange(name, text.value))
    }

    // toggle (checked/unchecked map to configured values; "" => remove param)
    const toggle = wrapper.querySelector<HTMLInputElement>('input.vibe-toggle')
    if (toggle) {
      const checkedValue = wrapper.getAttribute('data-vibe-checked-value') ?? 'true'
      const uncheckedValue = wrapper.getAttribute('data-vibe-unchecked-value') ?? ''
      toggle.addEventListener('change', () =>
        onChange(name, toggle.checked ? checkedValue : uncheckedValue),
      )
    }

    // button_group: each option carries its value in data-vibe-value.
    const buttons = wrapper.querySelectorAll<HTMLElement>('[data-vibe-value]')
    buttons.forEach((btn) => {
      btn.addEventListener('click', () => {
        const value = btn.getAttribute('data-vibe-value') ?? ''
        onChange(name, value)
      })
    })
  })
}
