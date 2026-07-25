#include "ft_list.h"
#include <unistd.h>
#include <stdlib.h>

void	ft_list_remove_if(t_list **begin_list, void *data_ref, int (*cmp)(),
			void (*free_fct)(void *));

static void	put_str(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
}

static void	print_list_str(t_list *lst)
{
	while (lst)
	{
		put_str((char *)lst->data);
		write(1, "\n", 1);
		lst = lst->next;
	}
}

static int	ft_strcmp(char *a, char *b)
{
	int	i;

	i = 0;
	while (a[i] && a[i] == b[i])
		i++;
	return ((unsigned char)a[i] - (unsigned char)b[i]);
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

static void	cleanup(t_list *lst)
{
	t_list	*next;

	while (lst)
	{
		next = lst->next;
		free(lst->data);
		free(lst);
		lst = next;
	}
}

int	main(int argc, char **argv)
{
	t_list	*begin;
	t_list	*elem;
	int		i;

	if (argc < 2)
		return (0);
	begin = NULL;
	i = argc - 1;
	while (i >= 2)
	{
		elem = ft_create_elem(dup_str(argv[i]));
		elem->next = begin;
		begin = elem;
		i--;
	}
	ft_list_remove_if(&begin, argv[1], &ft_strcmp, &free_data);
	print_list_str(begin);
	cleanup(begin);
	return (0);
}
