import { useState, useEffect } from 'react'
import './App.css'

function App() {
  const [joke, setJoke] = useState('Загрузка...')
  const [source, setSource] = useState('')
  const [translation, setTranslation] = useState('')
  const [showTranslation, setShowTranslation] = useState(false)
  const [isRussian, setIsRussian] = useState(true)

  const loadJoke = async () => {
    setJoke('Загрузка...')
    setSource('')
    setShowTranslation(false)
    setTranslation('')
    
    try {
      const res = await fetch('/random-joke')
      if (!res.ok) throw new Error('Ошибка загрузки')
      const data = await res.json()
      setJoke(data.joke || 'Нет анекдота')
      setSource(data.source ? `Источник: ${data.source}` : '')
      
      // Простая проверка на русский текст
      setIsRussian(/[а-яА-ЯёЁ]/.test(data.joke))
    } catch (e) {
      setJoke('Ошибка загрузки анекдота')
    }
  }

  const translateJoke = async () => {
    if (!joke || joke === 'Загрузка...' || joke === 'Ошибка загрузки анекдота') return
    
    setShowTranslation(true)
    setTranslation('Перевод...')
    
    try {
      const res = await fetch('/translate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: joke })
      })
      if (!res.ok) throw new Error('Ошибка перевода')
      const data = await res.json()
      setTranslation(data.translation || 'Не удалось перевести')
    } catch (e) {
      setTranslation('Ошибка перевода')
    }
  }

  useEffect(() => {
    loadJoke()
  }, [])

  return (
    <div className="container">
      <h1>😂 Анекдот дня</h1>
      <div className="joke">{joke}</div>
      <div className="source">{source}</div>
      <div className="button-container">
        <button className="btn" onClick={loadJoke}>Следующий анекдот</button>
        {!isRussian && (
          <button className="btn" onClick={translateJoke}>Перевести</button>
        )}
      </div>
      {showTranslation && (
        <div className="translation">{translation}</div>
      )}
    </div>
  )
}

export default App
