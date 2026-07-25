#include "ft_list.h"
#include <unistd.h>
#include <stdlib.h>

void	ft_list_clear(t_list *begin_list, void (*free_fct)(void *));

static void	put_str(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
}

static void	free_data(void *data)
{
	free(data);
}

static char	*dup_str(char *s)
{
	int		len;
	int		i;
	char	*copy;

	len = 0;
	while (s[len])
		len++;
	copy = malloc(len + 1);
	i = 0;
	while (i <= len)
	{
		copy[i] = s[i];
		i++;
	}
	return (copy);
}

int	main(int argc, char **argv)
{
	t_list	*begin;
	t_list	*elem;
	int		i;

	begin = NULL;
	i = 1;
	while (i < argc)
	{
		elem = ft_create_elem(dup_str(argv[i]));
		elem->next = begin;
		begin = elem;
		i++;
	}
	ft_list_clear(begin, &free_data);
	put_str("OK\n");
	return (0);
}
