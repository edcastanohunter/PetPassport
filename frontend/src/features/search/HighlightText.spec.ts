import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import HighlightText from './HighlightText.vue'

describe('HighlightText.vue (PP-F3-FE-001)', () => {
  it('renders plain text with no markers', () => {
    const wrapper = mount(HighlightText, {
      props: { text: 'Luna Beagle' },
    })
    expect(wrapper.text()).toBe('Luna Beagle')
    expect(wrapper.find('mark').exists()).toBe(false)
  })

  it('renders mark tag around highlighted terms without raw HTML injection', () => {
    const wrapper = mount(HighlightText, {
      props: { text: 'Mascota [[mark]]Luna[[/mark]] es un [[mark]]Beagle[[/mark]] activo' },
    })
    const marks = wrapper.findAll('mark')
    expect(marks).toHaveLength(2)
    expect(marks[0].text()).toBe('Luna')
    expect(marks[1].text()).toBe('Beagle')
    expect(wrapper.text()).toBe('Mascota Luna es un Beagle activo')
  })

  it('safely escapes potential HTML tags', () => {
    const wrapper = mount(HighlightText, {
      props: { text: '<script>alert(1)</script> [[mark]]<img src=x onerror=alert(1)>[[/mark]]' },
    })
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('mark').text()).toBe('<img src=x onerror=alert(1)>')
  })

  it('handles empty text gracefully', () => {
    const wrapper = mount(HighlightText, {
      props: { text: '' },
    })
    expect(wrapper.text()).toBe('')
  })
})
